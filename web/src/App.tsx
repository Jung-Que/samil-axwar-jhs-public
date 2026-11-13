import { useCallback, useEffect, useState } from "react";
import { ApiError, api, currentRole, setRole, setSession } from "./api";
import type { Department, LaneAction, View } from "./types";
import { SessionCompareView, SimulationView } from "./sessionViews";
import { AssumptionsView, CapacityView, ChurnView, CompareView, DecisionsView, LedgerView, LogView, SourceGapView, TasksView } from "./panels";
import { JudgeView } from "./judgeViews";
import { EventPanel, RequestsView } from "./EventPanel";
import { BacktestView, InboxView, LanesView, LineBoardView } from "./asofViews";
import { MyWorkView } from "./roleViews";
import { canEvent, deptOf } from "./perm";
import { ClockTools, ModeBand, PageHead, SessionTools, SideNav, TopBar, pageById, pageFor, pathOf, type Page, type PageId } from "./frame";
import { Link, navigate, useLocation } from "./router";

// 바로 기록해도 되는 사건(입력 필드가 필요 없음). 나머지는 사건 입력 페이지로 보낸다.
const DIRECT: Record<string, { actor: string; payload: unknown }> = {
  E8_SUBMIT_APPROVAL: { actor: "영업", payload: { note: "업무 레인 보드에서 상신" } },
  E7_SUBMIT_PROPOSAL: { actor: "영업", payload: {} },
};

// W05 운영 서비스 화면(SPA). 주소마다 페이지 하나(이슈 #53): 내 업무 → 입찰 판단 → 결정 → 운영 상태 → 기록 → 시뮬레이션 → 근거.
// 상태(View)는 한 번 받아 모든 페이지가 함께 쓰고, 사건을 넣으면 서버가 돌려준 새 상태로 바꾼다.
export default function App() {
  const [v, setV] = useState<View | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [roleCode, setRoleCode] = useState(currentRole());
  const loc = useLocation();
  // 사건 입력 페이지에서 고를 사건(할 일에서 누른 사건). 주소 ?type=으로도 연다.
  const [focus, setFocus] = useState<{ event: string; n: number } | null>(() => {
    const t = new URLSearchParams(window.location.search).get("type");
    return t ? { event: t, n: Date.now() } : null;
  });

  // 처음 주소 정리: / → 내 업무, 예전 탭 해시(#judge 등) → 그 페이지
  useEffect(() => {
    const old = pageById(window.location.hash.slice(1));
    if (old) navigate(old.path, true);
    else if (loc.path === "/") navigate(pathOf("my"), true);
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
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
      .catch((e: Error) => setErr(`API 연결 실패: ${e.message}`));
  }, []);

  const run = useCallback(async (fn: () => Promise<View>) => {
    setBusy(true);
    setErr(null);
    try {
      const nv = await fn();
      if (nv?.session?.id) setSession(nv.session.id, nv.session.kind); // 다음 호출부터 이 세션을 쓴다
      setV(nv);
    } catch (e) {
      if (e instanceof ApiError && e.view) setV(e.view);
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }, []);

  // 페이지를 바꾸면 지난 페이지의 오류 띠는 지운다.
  useEffect(() => setErr(null), [loc.path]);

  if (!v) return <main className="center">{err ?? "불러오는 중…"}</main>;
  const dept: Department | undefined = deptOf(v, roleCode);
  const go = (id: PageId) => navigate(pathOf(id));
  const chooseRole = (code: string) => {
    setRole(code);
    setRoleCode(code);
    setErr(null);
    go("my");
  };
  const openEvent = (event: string) => {
    setFocus({ event, n: Date.now() });
    navigate(`${pathOf("event")}?type=${encodeURIComponent(event)}`);
  };
  // 판단·제안의 "시뮬레이션에서 먼저 보기": 기준 세션이면 지금 상태를 분기한 시뮬레이션 세션을 만들고 시뮬레이션 페이지로 간다.
  const trySim = () => {
    if (v.session.kind !== "main") return go("sim");
    run(() => api.fork(v.state.log?.length ?? 0, `${v.session.name} 시뮬레이션`)).then(() => go("sim"));
  };

  // 할 수 있는 일: 상태(서버)와 부서 권한(권한표)을 함께 본다
  const onAction = (a: LaneAction) => {
    if (!a.enabled) return;
    const req = (v.state.requests ?? []).find((r) => r.request_id === a.request_id);
    const chk = canEvent(v, dept, a.event, req);
    if (!chk.ok) {
      setErr(`권한 없음: ${chk.reason}`);
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
        return <MyWorkView v={v} dept={dept} onAction={onAction} onChoose={chooseRole} onFocus={openEvent} goTab={go} />;
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
        return <LanesView v={v} onAction={onAction} />;
      case "lines":
        return (<><LineBoardView v={v} /><SourceGapView v={v} /></>);
      case "log":
        return <LogView v={v} onFork={dept ? (seq) => run(() => api.fork(seq)).then(() => go("sim")) : undefined} busy={busy} />;
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
    }
  };

  return (
    <div className="shell">
      <TopBar v={v} run={run} busy={busy} dept={dept} roleCode={roleCode} onChoose={chooseRole} />
      <ModeBand v={v} run={run} busy={busy} />
      <div className="shell-body">
        <SideNav v={v} dept={dept} current={page?.id} />
        <main className="page">
          {err && <div className="error" role="alert">{err}</div>}
          {page ? (
            <>
              <PageHead page={page} />
              {body(page)}
            </>
          ) : (
            <section className="card">
              <h1>페이지를 찾을 수 없다</h1>
              <p className="small muted">주소 {loc.path} · <Link to={pathOf("my")}>내 업무로</Link></p>
            </section>
          )}
        </main>
      </div>
    </div>
  );
}

// 결정 페이지: 상신된 제안안이 있으면 결정(E2)을 넣고, 지난 결정을 본다. 결정은 CEO/이사회 권한이다.
function ApprovalsPage({ v, run, dept }: { v: View; run: (fn: () => Promise<View>) => Promise<void>; dept?: Department }) {
  const bid = v.state.bid;
  const waiting = bid?.state === "상신";
  return (
    <>
      {waiting ? (
        <EventPanel v={v} run={run} dept={dept} only={["E2_DECISION"]} title={`결정 대기 · C-201 v${bid?.version ?? ""}`} />
      ) : (
        <section className="card">
          <h2>결정 대기 0</h2>
          <p className="small muted">
            입찰 상태 {bid ? `${bid.state} · v${bid.version}` : "접수 전"}. 영업이 제안안을 상신하면(E8) 여기서 결정한다.
          </p>
        </section>
      )}
      <DecisionsView v={v} />
    </>
  );
}
