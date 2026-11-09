# -*- coding: utf-8 -*-
"""자체 테스트 실행기. 외부 의존성 없음(Python 3.11+, Node 22+).

무엇을 돌릴지는 `docs/테스트_하네스.md`의 분기표가 정한다. 이 파일은 검사만 한다.

사용:
  python tools/selftest.py docs guard      # 그룹을 골라 실행
  python tools/selftest.py all             # devdb를 뺀 전부

그룹:
  docs      CLAUDE.md 줄 수, 개발 규칙 문서(CLAUDE·AGENTS·docs/) 속 경로 존재, 병합 충돌 표시
  analysis  개발 외 작업 폴더(analysis/, git 밖) 색인 경로 존재, README 등록. PR 없이 로컬에서 돌린다
  data      분석 스크립트를 다시 돌려 저장된 출력과 같은지(재현성)
  app       Go 서버(gofmt·vet·test·build), 화면 빌드(tsc + vite), 빌드한 서버 기동 후 /healthz · / · /api 응답,
            Dockerfile·.dockerignore 범위. 서버는 DB 없이(메모리 저장소) 127.0.0.1에 띄운다
  engine    W05 결정 모델: 원본 복사본(server/w05/data/raw) = SHA256SUMS = 원본, 업무모델 명세 기대값(§9),
            시연 3건 기대 상태(§10), AI 구조화(A1) 요청 형식·실패 처리
  devdb     로컬 Docker dev 테스트(compose.dev.yaml): 호스트 공개 포트 0개, Postgres 통합 테스트(go test),
            배포 이미지를 Postgres 모드로 띄워 /healthz · /api/status 확인. Docker Desktop 필요, all에서 빠진다
  guard     보호 경로·원본 데이터 불변, 올리면 안 되는 파일이 스테이징됐는지, analysis/가 git 밖인지,
            브랜치 규칙(훅 설치, 현재 브랜치 이름, main·dev에 직접 커밋 없음)

종료 코드: 0 통과, 1 실패.
"""
import difflib
import hashlib
import json
import os
import re
import shutil
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

# 개발 외 작업 폴더(리서치·문제 분석·기획·분석 스크립트·할 일). git에 올리지 않고(.git/info/exclude)
# 이슈·PR 없이 고친다. 규칙: docs/브랜치_규칙.md §0
ANALYSIS = "analysis"

# 경로가 틀리면 실패시키는 색인 문서. 나머지 문서는 경고만 한다.
STRICT_DOCS = ["CLAUDE.md", "AGENTS.md"]  # docs 그룹(+ STRICT_DIRS 아래 .md)
STRICT_DIRS = ["docs"]
ANALYSIS_STRICT = [f"{ANALYSIS}/README.md", f"{ANALYSIS}/TODO.md"]  # analysis 그룹
# 경로처럼 보이지만 참조가 아닌 이름(예: "열지 않는다"고 적은 파일).
NOT_PATHS = {"auth.json"}
# 설치·빌드 때 생기는 폴더(없을 수 있고, node_modules는 경로 수집에서도 빠진다).
NOT_PATHS |= {"web/node_modules", "web/node_modules/", "web/dist", "web/dist/"}

# 분석 스크립트 → 저장된 출력. 스크립트를 추가하면 여기와 하네스 문서에 함께 등록한다.
# 모두 `python <스크립트> <04_원본데이터 경로> <출력 경로>` 형식이다.
DATA_SCRIPTS = [
    ("analysis/05_데이터_연결/scripts/check_integrity.py", "analysis/05_데이터_연결/scripts/check_output.txt"),
    ("analysis/05_데이터_연결/scripts/profile_schema.py", "analysis/05_데이터_연결/스키마_프로파일.md"),
    ("analysis/06_W05_수주약속/scripts/w05_facts.py", "analysis/06_W05_수주약속/scripts/w05_facts_output.txt"),
]

# 스테이징되면 안 되는 경로(앞부분 일치)와 크기 한도.
FORBIDDEN_STAGED = [
    "01_참가자_배포용/",
    f"{ANALYSIS}/",  # git 밖 폴더. git add -f 로도 올리지 않는다
    "web/node_modules/",  # 화면 설치·빌드 산출물(web/.gitignore로 빠지지만 강제 추가도 막는다)
    "web/dist/",
    ".claude/settings.local.json",
    ".codex/hooks.json",
    ".agents/hooks.json",
    ".env",
]
MAX_FILE_MB = 20
PROTECTED_TRACKED = [".gitignore", ".claude", ".codex", ".agents"]

# 브랜치 규칙(docs/브랜치_규칙.md). 이름 정규식은 pre-push 훅의 BRANCH_RE 줄 한 곳에만 둔다.
HOOKS_DIR = "tools/git-hooks"
PRE_PUSH = ROOT / HOOKS_DIR / "pre-push"
LONG_LIVED = {"main", "dev"}  # PR 병합으로만 바뀌는 브랜치

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


def branch_re():
    if not PRE_PUSH.exists():
        return None
    m = re.search(r"^BRANCH_RE='([^']+)'", PRE_PUSH.read_text(encoding="utf-8"), re.M)
    return re.compile(m.group(1)) if m else None


BRANCH_NAME = branch_re()


# ---------------------------------------------------------------- docs

TICK = re.compile(r"`([^`\n]+)`")
PATH_EXT = re.compile(r"\.(md|py|txt|html|pdf|js|json|ya?ml|csv|jpg|png|mp4)$", re.I)


COMMANDS = {"python", "node", "git", "gh", "ffmpeg", "npm", "cd", "ls", "axhub", "curl"}
PLANNED = "(예정)"  # 문서에서 `경로`(예정)처럼 쓰면 아직 없는 경로로 보고 건너뛴다


def path_like(s, strict=True, top_level=frozenset()):
    s = s.strip()
    if not s or s in NOT_PATHS or "://" in s or any(c in s for c in "~*<>|\"'$=(){}"):
        return False
    if s.startswith(("-", ".", "/", "#")):
        return False
    if BRANCH_NAME and BRANCH_NAME.match(s):  # 브랜치 이름(예: docs/5-branch-rules)은 경로가 아니다
        return False
    if " " in s and (s.split()[0] in COMMANDS or not PATH_EXT.search(s)):  # 명령어 예시
        return False
    has_ext, is_dir = bool(PATH_EXT.search(s)), s.endswith("/")
    if not strict:
        # 일반 문서는 `Lot/Batch` 같은 표기가 많아 "폴더/…확장자" 또는 "폴더/" 꼴만 본다.
        return "/" in s and (has_ext or is_dir)
    # 색인 문서: 확장자·끝 슬래시가 있거나, 첫 부분이 실제 최상위 폴더·파일일 때만 경로로 본다
    # (`미확정/잠정` 같은 상태 표기 제외).
    return has_ext or is_dir or ("/" in s and s.split("/")[0] in top_level)


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


def check_doc_paths(group, strict, others):
    """strict 문서의 없는 경로는 실패, others 문서의 없는 경로는 경고."""
    paths = all_paths()
    top = frozenset(p for p in paths if "/" not in p)
    miss_strict, miss_other = [], []
    for docs, bucket, is_strict in ((strict, miss_strict, True), (others, miss_other, False)):
        for doc in docs:
            doc_dir = Path(rel(doc)).parent.as_posix()
            for n, line in enumerate(doc.read_text(encoding="utf-8").splitlines(), 1):
                for m in TICK.finditer(line):
                    cand = m.group(1)
                    if line[m.end():].startswith(PLANNED):
                        continue
                    if path_like(cand, is_strict, top) and not resolves(cand, doc_dir, paths):
                        bucket.append(f"{rel(doc)}:{n} `{cand}`")
    for m in miss_other:
        warn(group, f"경로 없음(참고) {m}")
    report(group, not miss_strict,
           f"색인 문서 {len(strict)}개 경로 확인" + ("" if not miss_strict else " — 없는 경로:\n    " + "\n    ".join(miss_strict)))


def span_regex(span):
    s = re.sub(r"[A-Za-z]*\d+~[A-Za-z]*\d+", "QQRANGEQQ", span)  # W01~W08
    s = re.sub(r"<[^>]+>", "QQDIRQQ", s)  # <트랙>
    s = re.escape(s).replace(r"\*", "[^/]*")
    return s.replace("QQRANGEQQ", r"[A-Za-z]*\d+").replace("QQDIRQQ", "[^/]+")


def check_analysis_registry():
    base = ROOT / ANALYSIS
    readme = base / "README.md"
    # 등록은 표의 행(`|`로 시작)에 적힌 경로만 인정한다. 본문 언급(예: 핵심 결론의 폴더 경로)은 등록이 아니다.
    rows = [ln for ln in readme.read_text(encoding="utf-8").splitlines() if ln.lstrip().startswith("|")]
    spans = TICK.findall("\n".join(rows))
    rx_dir = [re.compile("^" + span_regex(s)) for s in spans if s.endswith("/")]
    rx_file = [re.compile("(^|/)" + span_regex(s) + "$") for s in spans if not s.endswith("/")]
    missing = []
    for p in walk(base):
        r = p.relative_to(base).as_posix()
        if r == "README.md":
            continue
        if not any(x.match(r) for x in rx_dir) and not any(x.search(r) for x in rx_file):
            missing.append(r)
    report("analysis", not missing,
           f"{ANALYSIS} 파일이 모두 {ANALYSIS}/README.md에 등록됨" if not missing
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
    strict = [ROOT / d for d in STRICT_DOCS if (ROOT / d).exists()]
    for d in STRICT_DIRS:
        strict += [p for p in walk(ROOT / d) if p.suffix == ".md"]
    check_doc_paths("docs", strict, [])
    check_conflict_markers()


def run_analysis():
    base = ROOT / ANALYSIS
    if not (base / "README.md").exists():
        report("analysis", False, f"{ANALYSIS}/README.md 없음 — 이 폴더는 git 밖이라 이 PC에만 있다")
        return
    strict = [ROOT / d for d in ANALYSIS_STRICT if (ROOT / d).exists()]
    strict_set = {rel(p) for p in strict}
    others = [p for p in walk(base) if p.suffix == ".md" and rel(p) not in strict_set]
    check_doc_paths("analysis", strict, others)
    check_analysis_registry()


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


SERVER = ROOT / "server"  # Go 서버(표준 라이브러리만)
WEB = ROOT / "web"  # React + TypeScript 화면(Vite 빌드)


def http_fetch(url):
    try:
        with urllib.request.urlopen(url, timeout=2) as r:
            return r.status, r.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace")
    except OSError:
        return None, ""


def run_tool(label, args, cwd, timeout=300, group="app"):
    """명령을 돌려 그룹에 결과를 보고한다. Windows의 npm.cmd도 찾도록 shutil.which로 실행 파일을 고른다."""
    exe = shutil.which(args[0])
    if not exe:
        report(group, False, f"{label}: '{args[0]}' 명령이 없다")
        return False, ""
    try:
        r = subprocess.run([exe, *args[1:]], cwd=cwd, capture_output=True, text=True,
                           encoding="utf-8", errors="replace", timeout=timeout)
    except subprocess.TimeoutExpired:
        report(group, False, f"{label}: {timeout}초 안에 끝나지 않음")
        return False, ""
    ok = r.returncode == 0
    tail = (r.stdout + r.stderr).strip()[-800:]
    report(group, ok, label if ok else f"{label} 실패\n    " + tail.replace("\n", "\n    "))
    return ok, r.stdout


def check_dockerfile():
    # 빌드 단계가 server/·web/ 폴더 전체를 복사하는지(web/package.json만 복사하면 소스가 빠진다),
    # .dockerignore가 둘만 보내고 로컬 설치물은 빼는지 본다.
    copied = set()
    for line in (ROOT / "Dockerfile").read_text(encoding="utf-8").splitlines():
        parts = line.split()
        if parts and parts[0].upper() == "COPY" and not any(p.startswith("--from") for p in parts):
            copied.update(x.rstrip("/") for x in parts[1:-1] if not x.startswith("--"))
    missing = sorted({"server", "web"} - copied)
    di = ROOT / ".dockerignore"
    rules = {ln.strip() for ln in di.read_text(encoding="utf-8").splitlines()} if di.exists() else set()
    lacking = sorted({"*", "!server", "!web", "web/node_modules", "web/dist"} - rules)
    ok = not missing and not lacking
    report("app", ok, f"Dockerfile 복사 {sorted(copied)}, .dockerignore는 server·web만 보냄" if ok
           else f"Dockerfile 복사 빠짐 {missing}, .dockerignore에 필요한 줄 없음 {lacking}")


def serve_check(exe, dist):
    # 빌드한 서버를 빈 포트로 띄워 배포와 같은 경로를 확인한다. DB 주소는 지워 메모리 저장소로 띄운다.
    port = free_port()
    env = {k: v for k, v in os.environ.items() if k not in ("DATABASE_URL", "DIRECT_DATABASE_URL")}
    env.update(PORT=str(port), WEB_DIR=str(dist), HOST="127.0.0.1")
    proc = subprocess.Popen([str(exe)], cwd=SERVER, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        base = f"http://127.0.0.1:{port}"
        status, deadline = None, time.time() + 15
        while time.time() < deadline and status is None:
            if proc.poll() is not None:
                break
            status, _ = http_fetch(base + "/healthz")
            if status is None:
                time.sleep(0.2)
        report("app", status == 200, f"서버 기동 /healthz → {status}")
        if status != 200:
            return
        for path, want, has in (("/", 200, 'id="root"'),
                                ("/api/status", 200, '"stage"'),
                                ("/api/status", 200, '"store":"memory"'),
                                ("/api/__selftest_404__", 404, '"error"'),
                                ("/__selftest_route__", 200, 'id="root"')):  # 화면 주소 새로고침
            got, body = http_fetch(base + path)
            report("app", got == want and has in body, f"GET {path} → {got} (기대 {want}, {has} 포함)")
    finally:
        proc.terminate()
        try:
            proc.wait(5)
        except subprocess.TimeoutExpired:
            proc.kill()


def run_app():
    gofmt = shutil.which("gofmt")
    if not gofmt or not shutil.which("go"):
        report("app", False, "go·gofmt 명령이 없다(Go 1.26 이상 필요)")
    else:
        r = subprocess.run([gofmt, "-l", "."], cwd=SERVER, capture_output=True, text=True, encoding="utf-8")
        unformatted = r.stdout.strip()
        report("app", r.returncode == 0 and not unformatted,
               "gofmt 형식 맞음" if not unformatted else f"gofmt로 고칠 파일: {unformatted}")
        run_tool("go vet", ["go", "vet", "./..."], SERVER)
        run_tool("go test", ["go", "test", "./..."], SERVER)

    with tempfile.TemporaryDirectory() as tmp:
        exe = Path(tmp) / ("server.exe" if os.name == "nt" else "server")
        built, _ = run_tool("go build", ["go", "build", "-o", str(exe), "."], SERVER) if shutil.which("go") else (False, "")
        dist = Path(tmp) / "dist"  # 작업 폴더의 web/dist를 건드리지 않게 임시 폴더로 빌드한다
        if not (WEB / "node_modules").exists():
            report("app", False, "web/node_modules 없음 → cd web && npm ci")
            web_ok = False
        else:
            web_ok, _ = run_tool("화면 빌드(tsc + vite build)",
                                 ["npm", "run", "build", "--", "--outDir", str(dist), "--emptyOutDir"], WEB)
        if built and web_ok:
            serve_check(exe, dist)
    check_dockerfile()


# ---------------------------------------------------------------- engine

W05_RAW = SERVER / "w05" / "data" / "raw"  # W05 원본 복사본(수정 없음, go:embed로 바이너리에 포함)


def sha256_file(p):
    return hashlib.sha256(p.read_bytes()).hexdigest()


def run_engine():
    # 1) 복사본 = SHA256SUMS = 이 PC의 원본(원본이 있을 때). 복사본을 손으로 고치면 여기서 잡힌다.
    sums = W05_RAW / "SHA256SUMS"
    if not sums.exists():
        report("engine", False, f"{rel(sums)} 없음")
    else:
        listed = [ln.split("  ", 1) for ln in sums.read_text(encoding="utf-8").splitlines() if ln.strip()]
        bad = []
        for h, name in listed:
            cp = W05_RAW / name
            if not cp.exists() or sha256_file(cp) != h:
                bad.append(f"{name}: 복사본이 SHA256SUMS와 다름")
            if RAW.exists() and (not (RAW / name).exists() or sha256_file(RAW / name) != h):
                bad.append(f"{name}: 원본(01_참가자_배포용/04_원본데이터)과 다름")
        names = {n for _, n in listed}
        extra = [p.relative_to(W05_RAW).as_posix() for p in W05_RAW.rglob("*")
                 if p.is_file() and p.name != "SHA256SUMS" and p.relative_to(W05_RAW).as_posix() not in names]
        bad += [f"{x}: SHA256SUMS에 없는 파일" for x in extra]
        against = " = 원본" if RAW.exists() else " (원본 없는 PC라 원본 비교 생략)"
        report("engine", not bad, f"W05 원본 복사본 {len(listed)}개 = SHA256SUMS{against}" if not bad
               else "복사본 검사 실패:\n    " + "\n    ".join(bad))
    if not shutil.which("go"):
        report("engine", False, "go 명령이 없다(Go 1.26 이상 필요)")
        return
    # 2) 엔진 기대값(업무모델 명세 §9)과 시연 3건 기대 상태(§10)
    run_tool("엔진 기대값(명세 §9: 여유·Masil·적격일·부족·지체상금·경제성)",
             ["go", "test", "-count=1", "-run", "Manifest|Oracle", "./w05"], SERVER, group="engine")
    run_tool("시연 3건 기대 상태(명세 §10: 정상·변경·정보 부족·거절, 차단 기록)",
             ["go", "test", "-count=1", "-run", "Scenario|InvalidAndBlocked", "./w05"], SERVER, group="engine")
    run_tool("AI 구조화(A1) 요청 형식·실패 처리(가짜 서버)",
             ["go", "test", "-count=1", "-run", "Structure", "./w05"], SERVER, group="engine")


# ---------------------------------------------------------------- devdb

COMPOSE_FILE = ROOT / "compose.dev.yaml"  # 로컬 Docker dev 테스트(포트 공개 없음)
COMPOSE = ["docker", "compose", "-f", str(COMPOSE_FILE)]


def compose(*args, timeout=900):
    return subprocess.run([*COMPOSE, *args], cwd=ROOT, capture_output=True, text=True,
                          encoding="utf-8", errors="replace", timeout=timeout)


def check_no_published_ports():
    # compose 설정을 풀어 본 뒤 어떤 서비스에도 ports(호스트 공개)가 없는지 본다.
    r = compose("config", "--format", "json", timeout=60)
    if r.returncode != 0:
        report("devdb", False, "compose 설정 읽기 실패\n    " + r.stderr.strip()[-400:])
        return False
    services = json.loads(r.stdout).get("services", {})
    exposed = {name: s["ports"] for name, s in services.items() if s.get("ports")}
    report("devdb", not exposed, f"compose 서비스 {len(services)}개, 호스트 공개 포트 0개" if not exposed
           else f"호스트에 포트를 공개하는 서비스: {exposed}")
    return not exposed


def run_devdb():
    if not shutil.which("docker"):
        report("devdb", False, "docker 명령이 없다(Docker Desktop 필요)")
        return
    info = subprocess.run(["docker", "info", "--format", "{{.ServerVersion}}"],
                          capture_output=True, text=True, encoding="utf-8", errors="replace")
    if info.returncode != 0 or not info.stdout.strip():
        report("devdb", False, "Docker 엔진이 꺼져 있다 → Docker Desktop을 켜고 다시 실행")
        return
    if not check_no_published_ports():
        return
    try:
        run_tool("Postgres 통합 테스트(go test, Docker 안)", [*COMPOSE, "run", "--rm", "test"], ROOT,
                 timeout=900, group="devdb")
        up = compose("up", "-d", "--build", "--wait", "app")
        report("devdb", up.returncode == 0, "배포 이미지 빌드·기동(Postgres 모드), healthcheck 통과"
               if up.returncode == 0 else "배포 이미지 빌드·기동 실패\n    "
               + (up.stdout + up.stderr).strip()[-800:].replace("\n", "\n    "))
        if up.returncode != 0:
            return
        r = compose("run", "--rm", "smoke", timeout=120)
        for want in ("ok", '"store":"postgres"', '"db":"ok"', '"schema":'):
            ok = r.returncode == 0 and want in r.stdout
            report("devdb", ok, f"컨테이너 안에서 app 호출 → {want} 포함" if ok
                   else f"app 응답에 {want} 없음: {(r.stdout + r.stderr).strip()[-300:]}")
        # 실제로 떠 있는 컨테이너에도 호스트 공개 포트가 없는지 본다(Publishers의 PublishedPort가 0이 아닌 것).
        published = []
        for line in compose("ps", "--format", "json", timeout=60).stdout.splitlines():
            if line.strip():
                c = json.loads(line)
                published += [p for p in c.get("Publishers") or [] if p.get("PublishedPort")]
        report("devdb", not published, "실행 중 컨테이너의 호스트 공개 포트 0개" if not published
               else f"공개된 포트가 있다: {published[:3]}")
    finally:
        compose("down", "-v", "--remove-orphans", timeout=300)


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
    check_analysis_outside_git()
    check_branch_rules(staged)


def check_analysis_outside_git():
    # 추적 중인 파일이 하나라도 있으면 실패. 무시 설정(.git/info/exclude의 /analysis/)이 빠져도 실패.
    tracked = [s for s in git("ls-files", "-z", "--", ANALYSIS).split("\0") if s]
    # --no-index: 이미 추적 중인 파일도 무시 규칙에 걸리는지 본다(없으면 추적 파일은 늘 '무시 안 됨'으로 나온다).
    ignored = subprocess.run(["git", "check-ignore", "-q", "--no-index", f"{ANALYSIS}/README.md"],
                             cwd=ROOT).returncode == 0
    ok = not tracked and ignored
    report("guard", ok, f"{ANALYSIS}/는 git 밖(추적 0개, 무시 설정 있음)" if ok
           else f"{ANALYSIS}/가 git 밖이 아님: 추적 {len(tracked)}개{' ' + str(tracked[:3]) if tracked else ''}, "
                f"무시 설정 {'있음' if ignored else '없음 → .git/info/exclude에 /' + ANALYSIS + '/ 추가'}")


def check_branch_rules(staged):
    # 훅은 sh가 읽으므로 CRLF가 섞이면 동작하지 않는다(.gitattributes로 LF 고정).
    hook_ok = BRANCH_NAME is not None and b"\r" not in PRE_PUSH.read_bytes()
    report("guard", hook_ok, f"pre-push 훅·이름 정규식 있음, LF 줄바꿈({HOOKS_DIR}/pre-push)" if hook_ok
           else f"{HOOKS_DIR}/pre-push가 없거나 BRANCH_RE 줄이 없거나 CRLF가 섞임")

    hp = git("config", "--get", "core.hooksPath").strip()
    report("guard", hp == HOOKS_DIR, f"훅 설치됨(core.hooksPath={hp})" if hp == HOOKS_DIR
           else f"훅 미설치(core.hooksPath='{hp}') → git config core.hooksPath {HOOKS_DIR}")

    br = git("rev-parse", "--abbrev-ref", "HEAD").strip()
    if br in LONG_LIVED:
        report("guard", not staged, f"{br} 브랜치, 스테이징 없음" if not staged
               else f"{br}에 직접 커밋하지 않는다(스테이징 {len(staged)}개) → dev에서 작업 브랜치를 만들어 옮긴다")
    elif br == "HEAD":
        warn("guard", "브랜치가 아닌 상태(detached HEAD) — 브랜치 이름 검사 건너뜀")
    elif BRANCH_NAME:
        ok = bool(BRANCH_NAME.match(br))
        report("guard", ok, f"브랜치 이름 규칙 맞음({br})" if ok
               else f"브랜치 이름 규칙 위반: {br} → <종류>/<이슈번호>-<영문-slug> (docs/브랜치_규칙.md §2)")


# ---------------------------------------------------------------- main

GROUPS = {"docs": run_docs, "analysis": run_analysis, "data": run_data, "app": run_app, "engine": run_engine,
          "guard": run_guard, "devdb": run_devdb}
NOT_IN_ALL = {"devdb"}  # Docker Desktop이 필요해 all에서 뺀다. 필요한 PR에서 따로 고른다(하네스 분기표)


def main(argv):
    sys.stdout.reconfigure(encoding="utf-8")
    picked = [g for g in GROUPS if g not in NOT_IN_ALL] if argv == ["all"] else argv
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
