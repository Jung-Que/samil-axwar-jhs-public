# -*- coding: utf-8 -*-
"""자체 테스트 실행기. 외부 의존성 없음(Python 3.11+, Node 22+).

무엇을 돌릴지는 `docs/테스트_하네스.md`의 분기표가 정한다. 이 파일은 검사만 한다.

사용:
  python tools/selftest.py docs guard      # 그룹을 골라 실행
  python tools/selftest.py all             # 전부

그룹:
  docs   CLAUDE.md 줄 수, 문서 속 경로 존재, research 목록 등록, 병합 충돌 표시
  data   분석 스크립트를 다시 돌려 저장된 출력과 같은지(재현성)
  app    JS 문법, 서버 기동 후 /healthz · / 응답, Dockerfile 복사 범위
  guard  보호 경로·원본 데이터 불변, 올리면 안 되는 파일이 스테이징됐는지

종료 코드: 0 통과, 1 실패.
"""
import difflib
import hashlib
import os
import re
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
RAW = ROOT / "01_참가자_배포용" / "04_원본데이터"
# 원본 데이터는 저장소 밖(이 PC 전용)이라 기준 해시도 로컬(.git 안)에 둔다.
RAW_BASELINE = ROOT / ".git" / "info" / "raw_data.sha256"

# 기록 키가 든 폴더는 내용을 읽지 않는다. 경로 이름 목록에도 넣지 않는다.
NEVER_WALK = {".git", "node_modules", ".claude", ".codex", ".agents", ".axhub"}
TEXT_EXT = {".md", ".py", ".js", ".mjs", ".html", ".css", ".json", ".yaml", ".yml", ".txt"}
CLAUDE_LIMIT = 100
CLAUDE_EXCLUDED_SECTION = "## 진행 중인 작업"

# 경로가 틀리면 실패시키는 색인 문서. 나머지 문서는 경고만 한다.
STRICT_DOCS = ["CLAUDE.md", "AGENTS.md", "TODO.md", "research/README.md"]
STRICT_DIRS = ["docs"]
# 경로처럼 보이지만 참조가 아닌 이름(예: "열지 않는다"고 적은 파일).
NOT_PATHS = {"auth.json"}

# 분석 스크립트 → 저장된 출력. 스크립트를 추가하면 여기와 하네스 문서에 함께 등록한다.
# 모두 `python <스크립트> <04_원본데이터 경로> <출력 경로>` 형식이다.
DATA_SCRIPTS = [
    ("research/05_데이터_연결/scripts/check_integrity.py", "research/05_데이터_연결/scripts/check_output.txt"),
    ("research/05_데이터_연결/scripts/profile_schema.py", "research/05_데이터_연결/스키마_프로파일.md"),
]

# 스테이징되면 안 되는 경로(앞부분 일치)와 크기 한도.
FORBIDDEN_STAGED = [
    "01_참가자_배포용/",
    "research/03_1기_수상작/videos/",
    ".claude/settings.local.json",
    ".codex/hooks.json",
    ".agents/hooks.json",
    ".env",
]
MAX_FILE_MB = 20
PROTECTED_TRACKED = [".gitignore", ".claude", ".codex", ".agents"]

results = []  # (그룹, 통과 여부, 메시지)


def report(group, ok, msg):
    results.append((group, ok, msg))
    print(f"[{'PASS' if ok else 'FAIL'}] {group}: {msg}")


def warn(group, msg):
    print(f"[WARN] {group}: {msg}")


def git(*args):
    r = subprocess.run(["git", *args], cwd=ROOT, capture_output=True, text=True, encoding="utf-8")
    return r.stdout


def walk(base, skip=NEVER_WALK):
    for dp, dns, fns in os.walk(base):
        dns[:] = sorted(d for d in dns if d not in skip)
        for f in sorted(fns):
            yield Path(dp) / f


def rel(p):
    return Path(p).relative_to(ROOT).as_posix()


# ---------------------------------------------------------------- docs

TICK = re.compile(r"`([^`\n]+)`")
PATH_EXT = re.compile(r"\.(md|py|txt|html|pdf|js|json|ya?ml|csv|jpg|png|mp4)$", re.I)


def path_like(s, strict=True):
    s = s.strip()
    if not s or s in NOT_PATHS or "://" in s or any(c in s for c in "~*<>|\"'$=(){}"):
        return False
    # 일반 문서는 `Lot/Batch` 같은 표기가 많아 "폴더/…확장자" 또는 "폴더/" 꼴만 본다.
    if not strict and not ("/" in s and (PATH_EXT.search(s) or s.endswith("/"))):
        return False
    if s.startswith(("-", ".", "/", "#")):
        return False
    if " " in s and not PATH_EXT.search(s):  # 명령어 예시
        return False
    return "/" in s or bool(PATH_EXT.search(s))


def all_paths():
    out = set()
    for dp, dns, fns in os.walk(ROOT):
        dns[:] = [d for d in dns if d not in NEVER_WALK]
        r = Path(dp).relative_to(ROOT).as_posix()
        if r != ".":
            out.add(r)
        for f in fns:
            out.add(f if r == "." else f"{r}/{f}")
    return out


def resolves(cand, doc_dir, paths):
    c = cand.strip().rstrip("/")
    for base in ("", doc_dir):
        p = os.path.normpath(os.path.join(base, c)).replace("\\", "/")
        if p in paths:
            return True
    # 줄여 쓴 경로(예: 00_common/x.md)는 끝부분 일치로 찾는다.
    return any(x.endswith("/" + c) for x in paths)


def check_claude_lines():
    lines = (ROOT / "CLAUDE.md").read_text(encoding="utf-8").splitlines()
    counted, skipping = 0, False
    for ln in lines:
        if ln.startswith("## "):
            skipping = ln.startswith(CLAUDE_EXCLUDED_SECTION)
        if not skipping:
            counted += 1
    report("docs", counted <= CLAUDE_LIMIT,
           f"CLAUDE.md {counted}/{CLAUDE_LIMIT}줄('진행 중인 작업' 제외, 전체 {len(lines)}줄)")


def check_doc_paths():
    paths = all_paths()
    strict = [ROOT / d for d in STRICT_DOCS if (ROOT / d).exists()]
    for d in STRICT_DIRS:
        strict += [p for p in walk(ROOT / d) if p.suffix == ".md"]
    strict_set = {rel(p) for p in strict}
    others = [p for p in walk(ROOT / "research") if p.suffix == ".md" and rel(p) not in strict_set]
    miss_strict, miss_other = [], []
    for docs, bucket, is_strict in ((strict, miss_strict, True), (others, miss_other, False)):
        for doc in docs:
            doc_dir = Path(rel(doc)).parent.as_posix()
            for n, line in enumerate(doc.read_text(encoding="utf-8").splitlines(), 1):
                for cand in TICK.findall(line):
                    if path_like(cand, is_strict) and not resolves(cand, doc_dir, paths):
                        bucket.append(f"{rel(doc)}:{n} `{cand}`")
    for m in miss_other:
        warn("docs", f"경로 없음(참고) {m}")
    report("docs", not miss_strict,
           f"색인 문서 {len(strict)}개 경로 확인" + ("" if not miss_strict else " — 없는 경로:\n    " + "\n    ".join(miss_strict)))


def span_regex(span):
    s = re.sub(r"[A-Za-z]*\d+~[A-Za-z]*\d+", "QQRANGEQQ", span)  # W01~W08
    s = re.sub(r"<[^>]+>", "QQDIRQQ", s)  # <트랙>
    s = re.escape(s).replace(r"\*", "[^/]*")
    return s.replace("QQRANGEQQ", r"[A-Za-z]*\d+").replace("QQDIRQQ", "[^/]+")


def check_research_registry():
    readme = ROOT / "research" / "README.md"
    spans = TICK.findall(readme.read_text(encoding="utf-8"))
    rx_dir = [re.compile("^" + span_regex(s)) for s in spans if s.endswith("/")]
    rx_file = [re.compile("(^|/)" + span_regex(s) + "$") for s in spans if not s.endswith("/")]
    missing = []
    for p in walk(ROOT / "research"):
        r = p.relative_to(ROOT / "research").as_posix()
        if r == "README.md":
            continue
        if not any(x.match(r) for x in rx_dir) and not any(x.search(r) for x in rx_file):
            missing.append(r)
    report("docs", not missing,
           "research 파일이 모두 research/README.md에 등록됨" if not missing
           else "README 미등록:\n    " + "\n    ".join(missing))


def check_conflict_markers():
    hits = []
    skip = NEVER_WALK | {"01_참가자_배포용", "videos"}
    for p in walk(ROOT, skip):
        if p.suffix.lower() not in TEXT_EXT:
            continue
        try:
            for n, line in enumerate(p.read_text(encoding="utf-8").splitlines(), 1):
                if line.startswith(("<<<<<<< ", ">>>>>>> ")):
                    hits.append(f"{rel(p)}:{n}")
        except UnicodeDecodeError:
            continue
    report("docs", not hits, "병합 충돌 표시 없음" if not hits else "충돌 표시: " + ", ".join(hits))


def run_docs():
    check_claude_lines()
    check_doc_paths()
    check_research_registry()
    check_conflict_markers()


# ---------------------------------------------------------------- data

def run_data():
    if not RAW.exists():
        report("data", False, f"원본 데이터 폴더 없음: {rel(RAW)}")
        return
    env = dict(os.environ, PYTHONIOENCODING="utf-8")
    with tempfile.TemporaryDirectory() as tmp:
        for script, saved in DATA_SCRIPTS:
            out = Path(tmp) / Path(saved).name
            r = subprocess.run([sys.executable, str(ROOT / script), str(RAW), str(out)],
                               cwd=ROOT, env=env, capture_output=True, text=True, encoding="utf-8")
            if r.returncode != 0:
                report("data", False, f"{script} 실행 실패\n{r.stderr[-800:]}")
                continue
            a = (ROOT / saved).read_text(encoding="utf-8").splitlines()
            b = out.read_text(encoding="utf-8").splitlines()
            if a == b:
                report("data", True, f"{Path(script).name} 재실행 = {saved}")
            else:
                diff = list(difflib.unified_diff(a, b, "저장본", "재실행", n=0, lineterm=""))[:12]
                report("data", False, f"{Path(script).name} 재실행 결과가 다름\n    " + "\n    ".join(diff))


# ---------------------------------------------------------------- app

def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def http_get(url):
    try:
        with urllib.request.urlopen(url, timeout=2) as r:
            return r.status
    except urllib.error.HTTPError as e:
        return e.code
    except OSError:
        return None


def check_dockerfile():
    text = (ROOT / "Dockerfile").read_text(encoding="utf-8")
    copied = set()
    for line in text.splitlines():
        parts = line.split()
        if parts and parts[0].upper() == "COPY":
            copied.update(Path(x).parts[0] for x in parts[1:-1] if not x.startswith("--"))
    server = (ROOT / "server.js").read_text(encoding="utf-8")
    used = set(re.findall(r"require\(\s*['\"]\./([^/'\"]+)", server))
    used |= set(re.findall(r"path\.join\(\s*__dirname\s*,\s*['\"]([^/'\"]+)", server))
    used |= {"server.js", "package.json"}
    missing = sorted(u for u in used if u not in copied)
    report("app", not missing,
           f"Dockerfile 복사 범위 {sorted(copied)}" + ("" if not missing else f" — 빠짐: {missing}"))


def run_app():
    js = [ROOT / "server.js"] + [p for p in walk(ROOT / "public") if p.suffix in (".js", ".mjs")]
    bad = []
    for f in js:
        r = subprocess.run(["node", "--check", str(f)], capture_output=True, text=True, encoding="utf-8")
        if r.returncode != 0:
            bad.append(f"{rel(f)}: {r.stderr.strip()[:300]}")
    report("app", not bad, f"JS 문법 {len(js)}개 파일" + ("" if not bad else "\n    " + "\n    ".join(bad)))

    port = free_port()
    proc = subprocess.Popen(["node", "server.js"], cwd=ROOT, env=dict(os.environ, PORT=str(port)),
                            stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
    try:
        base = f"http://127.0.0.1:{port}"
        status, deadline = None, time.time() + 10
        while time.time() < deadline and status is None:
            if proc.poll() is not None:
                break
            status = http_get(base + "/healthz")
            if status is None:
                time.sleep(0.2)
        report("app", status == 200, f"서버 기동 /healthz → {status}")
        if status == 200:
            for path, want in (("/", 200), ("/__selftest_404__", 404)):
                got = http_get(base + path)
                report("app", got == want, f"GET {path} → {got} (기대 {want})")
    finally:
        proc.terminate()
        try:
            proc.wait(5)
        except subprocess.TimeoutExpired:
            proc.kill()
    check_dockerfile()


# ---------------------------------------------------------------- guard

def raw_hashes():
    out = {}
    for p in walk(RAW, skip=set()):
        out[p.relative_to(RAW).as_posix()] = hashlib.sha256(p.read_bytes()).hexdigest()
    return out


def run_guard():
    changed = git("status", "--porcelain", "--", *PROTECTED_TRACKED).strip()
    report("guard", not changed, "보호 경로(.gitignore·.claude·.codex·.agents) 변경 없음"
           if not changed else "보호 경로 변경:\n    " + changed.replace("\n", "\n    "))

    if RAW.exists():
        now = raw_hashes()
        if not RAW_BASELINE.exists():
            RAW_BASELINE.write_text("".join(f"{h}  {k}\n" for k, h in sorted(now.items())), encoding="utf-8")
            report("guard", True, f"원본 데이터 기준 해시 생성({len(now)}개 파일) → {rel(RAW_BASELINE)}")
        else:
            base = {}
            for line in RAW_BASELINE.read_text(encoding="utf-8").splitlines():
                h, k = line.split("  ", 1)
                base[k] = h
            diff = sorted(k for k in base.keys() | now.keys() if base.get(k) != now.get(k))
            report("guard", not diff, f"원본 데이터 {len(now)}개 파일 불변" if not diff
                   else "원본 데이터 변경: " + ", ".join(diff[:10]))
    else:
        warn("guard", f"원본 데이터 폴더 없음({rel(RAW)}) — 불변 검사 건너뜀")

    staged = [s for s in git("diff", "--cached", "--name-only", "-z").split("\0") if s]
    bad = [s for s in staged if any(s == f.rstrip("/") or s.startswith(f) for f in FORBIDDEN_STAGED)]
    big = [s for s in staged if (ROOT / s).is_file() and (ROOT / s).stat().st_size > MAX_FILE_MB * 2**20]
    report("guard", not bad and not big, f"스테이징 {len(staged)}개 파일에 금지·대용량(>{MAX_FILE_MB}MB) 없음"
           if not bad and not big else f"금지 {bad} 대용량 {big}")


# ---------------------------------------------------------------- main

GROUPS = {"docs": run_docs, "data": run_data, "app": run_app, "guard": run_guard}


def main(argv):
    sys.stdout.reconfigure(encoding="utf-8")
    picked = list(GROUPS) if argv == ["all"] else argv
    unknown = [g for g in picked if g not in GROUPS]
    if not picked or unknown:
        print(__doc__)
        return 1
    for g in picked:
        GROUPS[g]()
    failed = [r for r in results if not r[1]]
    print(f"\n결과: {len(results) - len(failed)}개 통과, {len(failed)}개 실패 ({', '.join(picked)})")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
