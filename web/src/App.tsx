import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError, api, auth, onUnauthorized, setSession } from "./api";
import type { Department, LaneAction, User, View } from "./types";
import { SessionCompareView, SimulationView } from "./sessionViews";
import { AssumptionsView, CapacityView, ChurnView, CompareView, DecisionsView, LedgerView, LogView, SourceGapView, TasksView } from "./panels";
import { JudgeView } from "./judgeViews";
import { EventPanel, RequestsView } from "./EventPanel";
import { BacktestView, InboxView, LanesView, LineBoardView } from "./asofViews";
import { MyWorkView } from "./roleViews";
import { canEvent, deptOf } from "./perm";
import { ClockTools, ModeBand, PageHead, SessionTools, SideNav, TopBar, pageById, pageFor, pathOf, type Page, type PageId } from "./frame";
import { Link, navigate, useLocation } from "./router";
import { AccessPage, LoginPage } from "./login";
import { FlowBar, FlowPage } from "./flowViews";
import { BrandMark, EmptyState, Swan, type SwanName } from "./brand";

// 완료 알림(이슈 #59): 같은 세션에서 입찰 상태가 넘어가면 결과 한 줄과 백조를 보인다(위급·오류에는 쓰지 않는다).
type Notice = { swan: SwanName; title: string; desc: string; to?: string; toLabel?: string };

function noticeFor(prev: View, next: View): Notice | null {
  if (prev.session.id !== next.session.id) return null;
  const a = prev.state.bid?.state;
  const b = next.state.bid?.state;
  if (!b || a === b) return null;
  const last = next.state.log?.[next.state.log.length - 1]?.event.seq ?? 0;
  const made = (next.state.tasks ?? []).filter((t) => t.cause_seq === last && t.state === "열림");
  const madeText = made.length ? `작업 ${made.length}건이 생성되었습니다: ${made.map((t) => `${t.owner_role} ${t.title.split("(")[0].trim()}`).join(" · ")}` : "새로 생성된 작업이 없습니다.";
  if (b === "상신") return { swan: "waiting", title: "상신했습니다 · CEO/이사회 결정 대기", desc: "결정이 입력되면 시연 흐름과 결정 페이지에 표시됩니다.", to: "/flow", toLabel: "시연 흐름 보기" };
  if ((b === "승인" || b === "조건부 승인") && a === "상신") return { swan: "success", title: `결정을 기록했습니다 · ${b}`, desc: madeText, to: "/tasks", toLabel: "작업 보기" };
  if (b === "회신 대기" && (a === "승인" || a === "조건부 승인")) return { swan: "success", title: "제안을 제출했습니다", desc: madeText, to: "/tasks", toLabel: "작업 보기" };
  return null;
}

// 로그인 뒤 돌아갈 주소(?next=). 이 앱 안의 경로만 받는다(다른 사이트로 보내지 않게).
const safeNext = (n: string | null) => (n && n.startsWith("/") && !n.startsWith("//") && !n.startsWith("/login") ? n : null);

// 바로 기록해도 되는 사건(입력 필드가 필요 없음). 나머지는 사건 입력 페이지로 보낸다.
const DIRECT: Record<string, { actor: string; payload: unknown }> = {
  E8_SUBMIT_APPROVAL: { actor: "영업", payload: { note: "업무 레인 보드에서 상신" } },
  E7_SUBMIT_PROPOSAL: { actor: "영업", payload: {} },
};

// W05 운영 서비스 화면(SPA). 로그인(/login, 이슈 #55)부터 시작하고, 주소마다 페이지 하나(이슈 #53):
// 내 업무 → 입찰 판단 → 결정 → 운영 상태 → 기록 → 시뮬레이션 → 근거 → 관리.
// 상태(View)는 한 번 받아 모든 페이지가 함께 쓰고, 사건을 넣으면 서버가 돌려준 새 상태로 바꾼다.
export default function App() {
  const [user, setUser] = useState<User | null | undefined>(undefined); // undefined = 확인 중
  const [v, setV] = useState<View | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<Notice | null>(null);
  const vRef = useRef<View | null>(null);
  vRef.current = v;
  const loc = useLocation();
  // 사건 입력 페이지에서 고를 사건(할 일에서 누른 사건). 주소 ?type=으로도 연다.
  const [focus, setFocus] = useState<{ event: string; n: number } | null>(() => {
    const t = new URLSearchParams(window.location.search).get("type");
    return t ? { event: t, n: Date.now() } : null;
  });

  // 처음: 예전 탭 해시(#judge 등)는 그 페이지로 옮기고, 로그인했는지 본다. 로그인이 풀리면(401) 로그인 페이지로.
  useEffect(() => {
    const old = pageById(window.location.hash.slice(1));
    if (old) navigate(old.path, true);
    onUnauthorized(() => {
      setUser(null);
      setV(null);
    });
    auth
      .me()
      .then((r) => setUser(r.user))
      .catch(() => setUser(null));
  }, []);

  // 로그인 여부에 맞춰 주소를 맞춘다: 로그인 전에는 /login만, 로그인 뒤 /login·/ 이면 ?next= 또는 내 업무로.
  useEffect(() => {
    if (user === null && loc.path !== "/login") navigate("/login", true);
    else if (user && (loc.path === "/login" || loc.path === "/")) navigate(safeNext(loc.query.get("next")) ?? pathOf("my"), true);
  }, [user, loc]);

  // 사건 입력 페이지를 ?type=으로 열면 그 사건을 고른다(시연 흐름·할 일의 링크).
  useEffect(() => {
    const t = loc.query.get("type");
    if (loc.path === pathOf("event") && t) setFocus({ event: t, n: Date.now() });
  }, [loc]);

  useEffect(() => {
    if (!user) return;
    // 기억해 둔 세션이 지워졌으면(404) 기준 세션으로 연다.
    api
      .state()
      .catch((e: unknown) => {
        if (e instanceof ApiError && e.status === 404) {
          setSession("main", "main");
          return api.state();
        }
        throw e;
      })
      .then((nv) => {
        setSession(nv.session.id, nv.session.kind);
        setV(nv);
      })
      .catch((e: Error) => setErr(`운영 상태를 불러오지 못했습니다(${e.message}). 잠시 후 다시 시도해 주세요.`));
  }, [user?.login_id]); // eslint-disable-line react-hooks/exhaustive-deps

  // run은 API를 부르고 새 상태로 바꾼다. 성공 여부를 돌려줘 실패하면 페이지를 옮기지 않게 한다(오류 띠가 남도록).
  const run = useCallback(async (fn: () => Promise<View>): Promise<boolean> => {
    setBusy(true);
    setErr(null);
    try {
      const nv = await fn();
      if (nv?.session?.id) setSession(nv.session.id, nv.session.kind); // 다음 호출부터 이 세션을 쓴다
      const prev = vRef.current;
      setNotice(prev ? noticeFor(prev, nv) : null);
      setV(nv);
      return true;
    } catch (e) {
      if (e instanceof ApiError && e.view) setV(e.view);
      setErr(e instanceof Error ? e.message : String(e));
      return false;
    } finally {
      setBusy(false);
    }
  }, []);

  // 페이지를 바꾸면 지난 페이지의 오류 띠·완료 알림은 지운다.
  useEffect(() => {
    setErr(null);
    setNotice(null);
  }, [loc.path]);

  if (user === undefined) return <Loading />;
  if (user === null) return <LoginPage onLogin={(u) => { setErr(null); setUser(u); }} />;
  if (!v)
    return err ? (
      <main className="center">
        <EmptyState swan="error" title="서버에 연결하지 못했습니다" action={<button onClick={() => window.location.reload()}>다시 시도</button>}>
          {err}
        </EmptyState>
      </main>
    ) : (
      <Loading />
    );
  const dept: Department | undefined = deptOf(v, user.role_code);
  const go = (id: PageId) => navigate(pathOf(id));
  const logout = (next = "/login") => {
    auth.logout().finally(() => {
      setUser(null);
      setV(null);
      navigate(next);
    });
  };
  // "다음 차례" 부서 계정으로 다시 로그인: 로그인 페이지가 그 부서 계정을 미리 고르고, 로그인 뒤 next 페이지로 간다.
  const switchTo = (roleCode: string, next?: string) =>
    logout(`/login?role=${encodeURIComponent(roleCode)}${next ? `&next=${encodeURIComponent(next)}` : ""}`);
  const openEvent = (event: string) => {
    setFocus({ event, n: Date.now() });
    navigate(`${pathOf("event")}?type=${encodeURIComponent(event)}`);
  };
  // 판단·제안의 "시뮬레이션에서 먼저 보기": 기준 세션이면 지금 상태를 분기한 시뮬레이션 세션을 만들고 시뮬레이션 페이지로 간다.
  const trySim = () => {
    if (v.session.kind !== "main") return go("sim");
    run(() => api.fork(v.state.log?.length ?? 0, `${v.session.name} 시뮬레이션`)).then((ok) => ok && go("sim"));
  };

  // 할 수 있는 일: 상태(서버)와 부서 권한(권한표)을 함께 본다
  const onAction = (a: LaneAction) => {
    if (!a.enabled) return;
    const req = (v.state.requests ?? []).find((r) => r.request_id === a.request_id);
    const chk = canEvent(v, dept, a.event, req);
    if (!chk.ok) {
      setErr(chk.reason ?? "이 작업을 실행할 권한이 없습니다.");
      return;
    }
    if (DIRECT[a.event]) run(() => api.event(a.event, DIRECT[a.event].actor, DIRECT[a.event].payload));
    else if (a.event === "E3_REPLY") go("requests");
    else if (a.event === "E2_DECISION") go("approvals");
    else openEvent(a.event);
  };

  const page = pageFor(loc.path);
  const body = (p: Page) => {
    switch (p.id) {
      case "my":
        return <MyWorkView v={v} dept={dept} onAction={onAction} onChoose={(code) => switchTo(code, pathOf("my"))} onFocus={openEvent} goTab={go} />;
      case "flow":
        return <FlowPage v={v} user={user} dept={dept} onSwitch={switchTo} />;
      case "judge":
        return <JudgeView v={v} dept={dept} onAction={onAction} onTrySim={trySim} />;
      case "capacity":
        return (<><CapacityView v={v} /><CompareView v={v} /><SourceGapView v={v} /></>);
      case "inbox":
        return <InboxView v={v} />;
      case "approvals":
        return <ApprovalsPage v={v} run={run} dept={dept} />;
      case "requests":
        return <RequestsView v={v} run={run} dept={dept} />;
      case "tasks":
        return <TasksView v={v} />;
      case "ledger":
        return (<><LedgerView v={v} /><ChurnView v={v} /></>);
      case "lanes":
        return <LanesView v={v} onAction={onAction} dept={dept} />;
      case "lines":
        return <LineBoardView v={v} />;
      case "log":
        return <LogView v={v} onFork={dept ? (seq) => run(() => api.fork(seq)).then((ok) => { if (ok) go("sim"); }) : undefined} busy={busy} />;
      case "event":
        return (
          <div className="split">
            <div className="split-main"><EventPanel v={v} run={run} focus={focus} dept={dept} /></div>
            <div className="split-side"><ClockTools v={v} run={run} busy={busy} dept={dept} /></div>
          </div>
        );
      case "sim":
        return (<><SessionTools v={v} run={run} busy={busy} dept={dept} /><SimulationView v={v} run={run} busy={busy} dept={dept} goTab={go} /></>);
      case "sessions":
        return <SessionCompareView v={v} />;
      case "assume":
        return <AssumptionsView v={v} />;
      case "backtest":
        return <BacktestView v={v} />;
      case "access":
        return <AccessPage v={v} user={user} />;
    }
  };

  return (
    <div className="shell">
      <TopBar v={v} run={run} busy={busy} user={user} onLogout={() => logout()} />
      <ModeBand v={v} run={run} busy={busy} right={page?.id !== "flow" ? <FlowBar v={v} user={user} dept={dept} onSwitch={switchTo} /> : undefined} />
      <div className="shell-body">
        <SideNav v={v} dept={dept} current={page?.id} />
        <main className="page">
          {err && <div className="error" role="alert">{err}</div>}
          {notice && (
            <div className="notice" role="status">
              <Swan name={notice.swan} size={72} />
              <div className="notice-text">
                <p className="notice-title">{notice.title}</p>
                <p className="muted small">{notice.desc}</p>
              </div>
              <div className="row">
                {notice.to && <Link to={notice.to} className="btn small-btn">{notice.toLabel}</Link>}
                <button className="ghost small" onClick={() => setNotice(null)}>닫기</button>
              </div>
            </div>
          )}
          {page ? (
            <>
              <PageHead page={page} />
              {body(page)}
            </>
          ) : (
            <section className="card">
              <EmptyState swan="notfound" title="페이지를 찾을 수 없습니다" action={<Link to={pathOf("my")} className="btn">내 업무로</Link>}>
                {loc.path} — 주소가 변경되었거나 존재하지 않는 화면입니다.
              </EmptyState>
            </section>
          )}
        </main>
      </div>
    </div>
  );
}

// 결정 페이지: 상신된 제안안이 있으면 결정(E2)을 넣고, 지난 결정을 본다. 결정은 CEO/이사회 권한이다.
function ApprovalsPage({ v, run, dept }: { v: View; run: (fn: () => Promise<View>) => Promise<unknown>; dept?: Department }) {
  const bid = v.state.bid;
  const waiting = bid?.state === "상신";
  return (
    <>
      {waiting ? (
        <EventPanel v={v} run={run} dept={dept} only={["E2_DECISION"]} title={`결정 대기 · C-201 v${bid?.version ?? ""}`} />
      ) : (
        <section className="card">
          <EmptyState compact swan="waiting" title="결정 대기 0 · 상신을 기다리는 중" action={<Link to="/bids/C-201" className="btn ghost-btn">C-201 판단 보기</Link>}>
            현재 결정할 제안안이 없습니다(입찰 상태 {bid ? `${bid.state} · v${bid.version}` : "접수 전"}). 영업이 제안안을 상신하면(E8) 여기에 표시되며 CEO/이사회가 결정합니다.
          </EmptyState>
        </section>
      )}
      <DecisionsView v={v} />
    </>
  );
}

// 처음 불러오는 동안: 로고와 글자만(장식 그림은 쓰지 않는다)
function Loading() {
  return (
    <main className="center loading" aria-busy="true">
      <BrandMark size={44} />
      <span className="muted small">불러오는 중…</span>
    </main>
  );
}
