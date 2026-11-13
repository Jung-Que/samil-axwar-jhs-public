import { useEffect, useState, type ReactNode } from "react";
import { api } from "./api";
import type { Department, SessionInfo, User, View } from "./types";
import { canFeature, myWork } from "./perm";
import { Link } from "./router";

type Run = (fn: () => Promise<View>) => Promise<void>;

// 앱 틀(이슈 #53): 위 막대(세션·업무일·로그인한 사람·사건 입력) + 세션 띠 + 왼쪽 메뉴 + 본문 페이지.
// 한 페이지에 다 있던 탭 13개와 왼쪽 패널(사건 입력·작업)을 주소가 있는 페이지로 나눴다(SPA 유지, src/router.tsx).
// 메뉴 묶음은 v3.1 목업(내 업무 / 입찰 / 승인 / 운영 상태 / 기록 / 시뮬레이션 / 근거)을 따른다.

export const PAGES = [
  { id: "my", path: "/my", group: "", label: "내 업무", title: "내 업무", desc: "내 부서가 지금 할 일, 나에게 온 확인 요청과 작업" },
  { id: "judge", path: "/bids/C-201", group: "입찰", label: "C-201 판단·제안", title: "C-201 판단·제안", desc: "엔진 계산(능력 스냅샷 기준) · 월별 공급 · 플래그와 물을 곳" },
  { id: "capacity", path: "/bids/C-201/capacity", group: "입찰", label: "여유·증설안", title: "여유·증설안", desc: "라인 여유(R1), 증설·전환안 적격 능력일(R3), OPT-01 비교" },
  { id: "inbox", path: "/intake", group: "입찰", label: "접수함", title: "접수함", desc: "업무일까지 최근 며칠 동안 들어온 주문·신청(원본 기록)" },
  { id: "approvals", path: "/approvals", group: "승인", label: "결정", title: "결정", desc: "상신된 제안안의 참여·대안 결정(CEO/이사회)과 지난 결정" },
  { id: "requests", path: "/requests", group: "운영 상태", label: "확인 요청", title: "확인 요청", desc: "결정 모델이 부서·바이어에 묻는 것과 회신" },
  { id: "tasks", path: "/tasks", group: "운영 상태", label: "작업", title: "작업", desc: "사건이 만든 작업(원인 사건 번호와 담당 부서)" },
  { id: "ledger", path: "/ledger", group: "운영 상태", label: "약속 원장", title: "약속 원장", desc: "기존 B2B 약속·Masil 보호분·C-201 가예약과 이탈 신호" },
  { id: "lanes", path: "/lanes", group: "운영 상태", label: "업무 레인", title: "업무 레인", desc: "부서 레인별 들어온 것·할 수 있는 일·막힌 것" },
  { id: "lines", path: "/lines", group: "운영 상태", label: "생산 라인", title: "생산 라인", desc: "라인이 만드는 것과 지금 부하(배치 실적 기준)" },
  { id: "log", path: "/log", group: "기록", label: "사건 로그", title: "사건 로그", desc: "이 세션에 기록된 사건(상태는 이 로그를 다시 적용해 만든다)" },
  { id: "event", path: "/events/new", group: "기록", label: "사건 입력", title: "사건 입력", desc: "정해 둔 고정 값으로 사건을 넣는다(자유 문장·LLM 현재 제외)" },
  { id: "sim", path: "/sim", group: "시뮬레이션", label: "세션·모의 가정", title: "시뮬레이션", desc: "세션 만들기·분기·시연, 모의 가정(E9)과 민감도 격자" },
  { id: "sessions", path: "/sim/compare", group: "시뮬레이션", label: "세션 비교", title: "세션 비교", desc: "두 세션의 지표·제품×월·플래그·확인 요청·작업 차이" },
  { id: "assume", path: "/basis/assumptions", group: "근거", label: "가정", title: "가정", desc: "참가자 가정과 근거(원본 제약은 가정으로 없애지 않는다)" },
  { id: "backtest", path: "/basis/backtest", group: "근거", label: "정답 비교", title: "정답 비교", desc: "기준일 추정 납기 대 그 뒤 실제 출하(검증 전용)" },
  { id: "access", path: "/admin/access", group: "관리", label: "사용자·권한", title: "사용자·권한", desc: "로그인 계정과 역할별 권한(DB 표 app_users · app_roles · app_role_permissions)" },
] as const;

export type PageId = (typeof PAGES)[number]["id"];
export type Page = (typeof PAGES)[number];

export const pathOf = (id: PageId) => PAGES.find((p) => p.id === id)!.path;
export const pageFor = (path: string): Page | undefined => PAGES.find((p) => p.path === path);
// 예전 주소(#judge 등 탭 해시)를 새 페이지로 옮길 때 쓴다.
export const pageById = (id: string): Page | undefined => PAGES.find((p) => p.id === id);

// 입찰 상태 표시(팔레트, 이슈 #51): 진행 = 검정 채움 · 승인·수주 = 회색 채움 · 조건부·재검토·보류 = 빨강 테두리 · 실주 = 빨강 채움.
export function bidTone(state?: string): string {
  if (!state || state === "검토중") return "tone-gray";
  if (state === "승인" || state === "수주") return "tone-ok";
  if (state === "조건부 승인" || state === "재검토" || state === "보류") return "tone-warn";
  if (state === "실주") return "tone-bad";
  return "tone-accent";
}

const kindLabel = (s: SessionInfo) => (s.kind === "main" ? "기준" : "시뮬레이션");
const sessionLabel = (s: SessionInfo) => `${s.name} · ${kindLabel(s)}${s.events >= 0 ? ` · 사건 ${s.events}` : ""}`;

// ---- 위 막대 ----

type TopProps = { v: View; run: Run; busy: boolean; user: User; onLogout: () => void };

export function TopBar({ v, run, busy, user, onLogout }: TopProps) {
  const sessions = v.sessions ?? [v.session];
  return (
    <header className="topbar">
      <Link to="/my" className="brand">
        <b>W05 수주·약속</b>
        <span className="muted small">Corevia 영업·생산관리</span>
      </Link>
      <div className="top-field">
        <label className="top-label" htmlFor="top-session">세션</label>
        <select id="top-session" value={v.session.id} disabled={busy} onChange={(e) => run(() => api.open(e.target.value))}>
          {sessions.map((s) => (
            <option key={s.id} value={s.id}>{sessionLabel(s)}</option>
          ))}
        </select>
      </div>
      <div className="top-field">
        <span className="top-label">업무일</span>
        <b>{v.state.scenario.sim_date}</b>
        <span className="muted small">기준일 {v.as_of}</span>
      </div>
      <div className="top-spacer" />
      <Link to="/events/new" className="btn">+ 사건 입력</Link>
      <div className="top-user" aria-label="로그인한 사람">
        <span className="avatar" aria-hidden="true">{user.name.slice(0, 1)}</span>
        <span className="top-user-name">
          <b>{user.name}</b>
          <span className="muted small">{user.dept_label} · {user.login_id}</span>
        </span>
        <button className="ghost small" onClick={onLogout}>로그아웃</button>
      </div>
    </header>
  );
}

// 세션 띠: 기준 세션은 입력이 실제로 기록되고, 시뮬레이션 세션은 제출이 [모의]다(v3.1 목업, 같은 높이).
export function ModeBand({ v, run, busy }: { v: View; run: Run; busy: boolean }) {
  if (v.session.kind === "main") {
    return (
      <div className="band band-main" role="status">
        <b>기준 세션</b> · 이 세션의 입력은 실제로 기록된다 · 자기 부서 권한으로만 입력
      </div>
    );
  }
  const parent = (v.sessions ?? []).find((s) => s.id === v.session.parent_id);
  return (
    <div className="band band-sim" role="status">
      <b>시뮬레이션 세션 · {v.session.name}</b> · 제출은 [모의] · 다른 부서 대행 입력이 기록된다
      {v.session.parent_id && <span> · 분기: {parent?.name ?? v.session.parent_id} 사건 {v.session.fork_seq}까지</span>}
      <button className="small" disabled={busy} onClick={() => run(() => api.open("main"))}>기준 세션으로</button>
    </div>
  );
}

// ---- 왼쪽 메뉴 ----

export function SideNav({ v, dept, current }: { v: View; dept?: Department; current?: PageId }) {
  const w = myWork(v, dept);
  const openReqs = (v.state.requests ?? []).filter((r) => r.state !== "반영").length;
  const openTasks = (v.state.tasks ?? []).filter((t) => t.state === "열림").length;
  const bid = v.state.bid;
  const count: Partial<Record<PageId, number>> = {
    my: dept ? w.reqs.length + w.tasks.length + w.actions.filter((a) => a.enabled).length : undefined,
    approvals: bid?.state === "상신" ? 1 : 0,
    requests: openReqs,
    tasks: openTasks,
    log: v.state.log?.length ?? 0,
    sim: (v.sessions ?? []).filter((s) => s.kind === "sim").length,
  };
  const groups = [...new Set(PAGES.map((p) => p.group))];
  return (
    <nav className="sidenav" aria-label="메뉴">
      {groups.map((g) => (
        <div key={g || "home"} className="nav-group">
          {g && <h4>{g}</h4>}
          {PAGES.filter((p) => p.group === g).map((p) => (
            <Link key={p.id} to={p.path} className={p.id === current ? "on" : undefined} aria-current={p.id === current ? "page" : undefined}>
              <span>{p.label}</span>
              {p.id === "judge" ? (
                <span className={`pill tiny ${bidTone(bid?.state)}`}>{bid ? bid.state : "접수 전"}</span>
              ) : count[p.id] !== undefined ? (
                <span className="cnt">{count[p.id]}</span>
              ) : null}
            </Link>
          ))}
        </div>
      ))}
      <p className="nav-foot muted tiny">
        저장소 {v.store} · 원본 {v.raw_files_verified}개 해시 확인 · 입력 {v.input_mode}
      </p>
    </nav>
  );
}

// ---- 페이지 머리 ----

export function PageHead({ page, children }: { page: Page; children?: ReactNode }) {
  return (
    <div className="page-head">
      <div>
        <div className="crumb small muted">
          <Link to="/my">W05</Link>
          {page.group && <> / {page.group}</>}
        </div>
        <h1>{page.title}</h1>
        <p className="muted small">{page.desc}</p>
      </div>
      {children && <div className="row">{children}</div>}
    </div>
  );
}

// ---- 시뮬레이션 페이지의 세션 도구: 시연 3건·세션 초기화·삭제·다른 기준일로 새 세션 ----

const PRESETS = ["2024-12-31", "2025-06-30", "2025-12-31", "2026-03-31", "2026-06-30", "2026-07-01"];

export function SessionTools({ v, run, busy, dept }: { v: View; run: Run; busy: boolean; dept?: Department }) {
  const [pick, setPick] = useState(v.as_of);
  useEffect(() => setPick(v.as_of), [v.as_of]);
  const cur = v.session;
  const isMain = cur.kind === "main";
  const canReset = isMain ? canFeature(dept, "main.reset") : canFeature(dept, "sim.session");
  const canDrop = canFeature(dept, "session.delete");
  return (
    <div className="cols">
      <section className="card">
        <h2>시연 시나리오</h2>
        <p className="muted small">새 시뮬레이션 세션을 만들어 고정 입력값으로 끝까지 돌린다(지금 세션은 그대로) · 기준일 {v.as_of}</p>
        <div className="row">
          {v.scenarios.map((s) => (
            <button key={s.id} className="ghost" title={s.description} disabled={busy || !dept} onClick={() => run(() => api.scenario(s.id, v.as_of))}>{s.title}</button>
          ))}
        </div>
        {!dept && <p className="small bad">로그인한 계정의 역할을 권한표에서 찾지 못했다</p>}
        <ul className="tasks small">
          {v.scenarios.map((s) => <li key={s.id}><b>{s.title}</b> {s.description}</li>)}
        </ul>
      </section>
      <section className="card">
        <h2>지금 세션 · {cur.name}</h2>
        <p className="muted small">{isMain ? "기준 세션 초기화는 운영 관리만" : "시뮬레이션 세션은 만든 부서와 상관없이 정리할 수 있다(삭제는 운영 관리만)"}</p>
        <div className="row">
          <button className="ghost" disabled={busy || !canReset} title={canReset ? "지금 세션의 사건을 지우고 기준일부터 다시 시작한다" : "기준 세션 초기화는 운영 관리 부서만"} onClick={() => { if (window.confirm(`세션 "${cur.name}"의 사건을 모두 지운다`)) run(() => api.reset(v.as_of)); }}>이 세션 초기화</button>
          {!isMain && (
            <button className="ghost danger" disabled={busy || !canDrop} title={canDrop ? "" : "세션 삭제는 운영 관리 부서만"} onClick={() => { if (window.confirm(`세션 "${cur.name}"을 지운다`)) run(() => api.deleteSession(cur.id)); }}>세션 삭제</button>
          )}
        </div>
        <h3>다른 기준일로 새 세션</h3>
        <p className="muted small">그 날짜까지의 기록만 쓰는 새 시뮬레이션 세션 · 쓰는 기록 ~{v.cutoff} · 이사회 {v.board_date}</p>
        <div className="row">
          <input type="date" aria-label="기준일" min={v.as_of_min} max={v.as_of_max} value={pick} onChange={(e) => setPick(e.target.value)} />
          <button className="small" disabled={busy || !pick || !dept} title={dept ? "" : "로그인한 계정의 역할을 찾지 못했다"} onClick={() => run(() => api.newSession("", pick))}>시작</button>
        </div>
        <div className="row">
          {PRESETS.map((d) => (
            <button key={d} className={d === pick ? "ghost small active" : "ghost small"} disabled={busy} onClick={() => setPick(d)}>{d}</button>
          ))}
        </div>
        {v.state.evaluation.future_snapshot && <p className="small warn-text">미래 자료: 능력 스냅샷은 자료 마지막 날({v.data_end}) 것 하나뿐이다(T2)</p>}
      </section>
    </div>
  );
}

// ---- 사건 입력 페이지의 시계(E0) 바로 가기 ----

function addDays(d: string, n: number): string {
  const t = new Date(`${d}T00:00:00Z`);
  t.setUTCDate(t.getUTCDate() + n);
  return t.toISOString().slice(0, 10);
}
function addMonths(d: string, n: number): string {
  const t = new Date(`${d}T00:00:00Z`);
  t.setUTCMonth(t.getUTCMonth() + n);
  return t.toISOString().slice(0, 10);
}

export function ClockTools({ v, run, busy, dept }: { v: View; run: Run; busy: boolean; dept?: Department }) {
  const sim = v.state.scenario.sim_date;
  // 시계: 기준 세션은 운영 관리만, 시뮬레이션 세션은 모든 부서(시스템 대행)
  const canClock = v.session.kind === "main" ? canFeature(dept, "main.clock") : canFeature(dept, "sim.act");
  const why = canClock ? "" : v.session.kind === "main" ? "기준 세션 시계는 운영 관리 부서만" : "로그인한 계정의 역할을 찾지 못했다";
  const clock = (to: string) => run(() => api.event("E0_CLOCK", "시스템", { sim_date: to }));
  return (
    <section className="card">
      <h2>시계(E0)</h2>
      <p className="muted small">업무일 {sim} · 날짜를 앞으로 옮기면 기한 초과·재요청이 다시 계산된다{why && ` · ${why}`}</p>
      <div className="row">
        <button className="ghost small" disabled={busy || !canClock} title={why} onClick={() => clock(addDays(sim, 1))}>+1일</button>
        <button className="ghost small" disabled={busy || !canClock} title={why} onClick={() => clock(addDays(sim, 7))}>+1주</button>
        <button className="ghost small" disabled={busy || !canClock} title={why} onClick={() => clock(addMonths(sim, 1))}>+1달</button>
      </div>
    </section>
  );
}
