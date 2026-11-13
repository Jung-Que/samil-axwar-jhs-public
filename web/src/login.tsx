import { useEffect, useRef, useState, type FormEvent } from "react";
import { ApiError, auth } from "./api";
import type { AccessTables, Account, User, View } from "./types";
import { Section } from "./panels";
import { PermissionTable } from "./roleViews";

// 로그인 페이지(이슈 #55). 시연용: 부서마다 계정 하나(처음 뜰 때 서버가 넣는다), 회원 가입·비밀번호 변경은 없다.
// 주소 ?role=production 이면 그 부서 계정을 미리 고른다(입찰 흐름에서 "다음 차례" 부서로 다시 로그인할 때).
export function LoginPage({ onLogin }: { onLogin: (u: User) => void }) {
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [demoPw, setDemoPw] = useState<string | undefined>();
  const [id, setId] = useState("");
  const [pw, setPw] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const pwRef = useRef<HTMLInputElement>(null);
  const submitRef = useRef<HTMLButtonElement>(null);
  const wantRole = new URLSearchParams(window.location.search).get("role") ?? "";

  const pick = (a: Account, hint = demoPw) => {
    setId(a.login_id);
    setErr(null);
    if (hint) {
      setPw(hint);
      setTimeout(() => submitRef.current?.focus(), 0);
    } else {
      setPw("");
      setTimeout(() => pwRef.current?.focus(), 0);
    }
  };

  useEffect(() => {
    auth
      .accounts()
      .then((r) => {
        setAccounts(r.accounts);
        setDemoPw(r.demo_password);
        const a = r.accounts.find((x) => x.role_code === wantRole);
        if (a) pick(a, r.demo_password);
      })
      .catch((e: Error) => setErr(`계정 목록을 불러오지 못했다: ${e.message}`));
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      const r = await auth.login(id.trim(), pw);
      onLogin(r.user);
    } catch (x) {
      setErr(x instanceof ApiError ? x.message : String(x));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="login-wrap">
      <section className="login-card" aria-labelledby="login-title">
        <div className="login-brand">
          <b>W05 수주·약속 운영</b>
          <span className="muted small">Corevia 영업·생산관리 · Aurora Holdings</span>
        </div>
        <h1 id="login-title">로그인</h1>
        <form onSubmit={submit}>
          <label className="field">
            <span>아이디</span>
            <input name="username" autoComplete="username" value={id} onChange={(e) => setId(e.target.value)} required autoFocus />
          </label>
          <label className="field">
            <span>비밀번호</span>
            <input ref={pwRef} name="password" type="password" autoComplete="current-password" value={pw} onChange={(e) => setPw(e.target.value)} required />
          </label>
          {err && <p className="login-err small" role="alert">{err}</p>}
          <button ref={submitRef} type="submit" className="login-submit" disabled={busy || !id || !pw}>{busy ? "확인 중…" : "로그인"}</button>
        </form>
        <p className="muted tiny">시연용 로그인 · 계정·권한은 DB 표(app_users · app_roles · app_role_permissions)에서 읽는다 · 회원 가입·비밀번호 변경 없음</p>
      </section>

      <section className="card login-accounts" aria-labelledby="acc-title">
        <h2 id="acc-title">시연 계정</h2>
        <p className="muted small">
          부서마다 하나다. 고르면 아이디{demoPw ? "와 비밀번호" : ""}가 채워진다.
          {demoPw && <> 시연 비밀번호 <code>{demoPw}</code></>}
        </p>
        <div className="scroll">
          <table className="grid text">
            <thead>
              <tr><th>아이디</th><th>이름</th><th>부서</th><th>맡는 일</th><th /></tr>
            </thead>
            <tbody>
              {accounts.map((a) => (
                <tr key={a.login_id} className={a.login_id === id ? "row-strong" : ""}>
                  <td className="nowrap"><code>{a.login_id}</code></td>
                  <td className="nowrap">{a.name}</td>
                  <td className="nowrap">{a.dept_label}{a.role_code === wantRole && <span className="pill tiny tone-accent"> 다음 차례</span>}</td>
                  <td className="small">{a.duty}</td>
                  <td><button type="button" className="ghost small" aria-label={`${a.name} 계정 고르기`} onClick={() => pick(a)}>고르기</button></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}

// 관리 › 사용자·권한(이슈 #55): 계정 표와 역할별 권한 표. 조회만 한다(편집은 설계 단계).
export function AccessPage({ v, user }: { v: View; user: User }) {
  const [t, setT] = useState<AccessTables | null>(null);
  const [err, setErr] = useState<string | null>(null);
  useEffect(() => {
    auth.users().then(setT).catch((e: Error) => setErr(e.message));
  }, []);
  const duty = (code: string) => t?.roles.find((r) => r.code === code)?.duty ?? "";
  return (
    <>
      <Section title={`계정 (${t?.users.length ?? 0})`} mode={["real", "design"]} note={`app_users 표(저장소 ${t?.store ?? v.store}). 비밀번호는 해시만 저장한다. 계정 추가·수정·비활성은 설계 단계다.`}>
        {err && <p className="bad small">{err}</p>}
        <div className="scroll">
          <table className="grid text">
            <thead>
              <tr><th>아이디</th><th>이름</th><th>역할(부서)</th><th>맡는 일</th><th>만든 때</th></tr>
            </thead>
            <tbody>
              {(t?.users ?? []).map((u) => (
                <tr key={u.login_id} className={u.login_id === user.login_id ? "row-strong" : ""}>
                  <td className="nowrap"><code>{u.login_id}</code>{u.login_id === user.login_id && <span className="muted small"> · 지금 로그인</span>}</td>
                  <td className="nowrap">{u.name}</td>
                  <td className="nowrap">{u.dept_label} <span className="muted small">{u.role_code}</span></td>
                  <td className="small">{duty(u.role_code)}</td>
                  <td className="small nowrap">{u.created_at?.slice(0, 10) ?? ""}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Section>
      <Section title={`역할별 권한 (${t?.permissions.length ?? 0}줄)`} mode={["real"]} note="app_roles · app_role_permissions 표. 서버는 시작할 때 이 표를 읽어 판단한다(표가 비어 있으면 server/w05/permissions.go 기본값을 넣는다). 기준 세션은 자기 역할 권한으로만, 시뮬레이션 세션은 대행 입력을 기록하며 허용한다.">
        <PermissionTable v={v} current={user.role_code} />
      </Section>
    </>
  );
}
