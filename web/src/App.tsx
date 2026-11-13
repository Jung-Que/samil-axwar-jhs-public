import { useCallback, useEffect, useState } from "react";
import { ApiError, api, currentRole, setRole, setSession } from "./api";
import type { Department, LaneAction, View } from "./types";
import { SessionCompareView, SimulationView } from "./sessionViews";
import { AssumptionsView, CapacityView, ChurnView, CompareView, DecisionsView, LedgerView, LogView, SourceGapView, TasksView } from "./panels";
import { JudgeView } from "./judgeViews";
import { EventPanel, RequestsView } from "./EventPanel";
import { BacktestView, InboxView, LanesView, LineBoardView } from "./asofViews";
import { MyWorkView } from "./roleViews";
import { canEvent, canFeature, deptOf } from "./perm";
import { ALL_TABS, ContextBar, TabNav, type Tab } from "./frame";

// 바로 기록해도 되는 사건(입력 필드가 필요 없음). 나머지는 왼쪽 사건 입력으로 보낸다.
const DIRECT: Record<string, { actor: string; payload: unknown }> = {
  E8_SUBMIT_APPROVAL: { actor: "영업", payload: { note: "업무 레인 보드에서 상신" } },
  E7_SUBMIT_PROPOSAL: { actor: "영업", payload: {} },
};

// W05 운영 서비스 화면: 부서를 고르고(부서별 권한), 사건 입력 → 엔진 판단 → 사람 승인 → 제안·작업·상태 갱신.
export default function App() {
  const [v, setV] = useState<View | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [focus, setFocus] = useState<{ event: string; n: number } | null>(null);
  const [roleCode, setRoleCode] = useState(currentRole());
  // 탭은 주소 해시(#inbox 등)로도 연다. 시연 중 특정 화면을 바로 보여 주기 위해서다.
  const fromHash = () => {
    const h = window.location.hash.slice(1);
    return (ALL_TABS.find((t) => t === h) ?? "my") as Tab;
  };
  const [tab, setTabState] = useState<Tab>(fromHash);
  const setTab = (t: Tab) => {
    setTabState(t);
    window.history.replaceState(null, "", `${window.location.search}#${t}`);
  };

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

  if (!v) return <main className="center">{err ?? "불러오는 중…"}</main>;
  const dept: Department | undefined = deptOf(v, roleCode);
  const chooseRole = (code: string) => {
    setRole(code);
    setRoleCode(code);
    setErr(null);
    setTab("my");
  };
  const isMain = v.session.kind === "main";
  // 판단·제안의 "시뮬레이션에서 먼저 보기": 기준 세션이면 지금 상태를 분기한 시뮬레이션 세션을 만들고 시뮬레이션 탭으로 간다.
  const trySim = () => {
    if (!isMain) return setTab("sim");
    run(() => api.fork(v.state.log?.length ?? 0, `${v.session.name} 시뮬레이션`)).then(() => setTab("sim"));
  };
  const canReset = isMain ? canFeature(dept, "main.reset") : canFeature(dept, "sim.session");

  // 업무 레인 보드의 "할 수 있는 일": 상태(서버)와 부서 권한(권한표)을 함께 본다
  const onAction = (a: LaneAction) => {
    if (!a.enabled) return;
    const req = (v.state.requests ?? []).find((r) => r.request_id === a.request_id);
    const chk = canEvent(v, dept, a.event, req);
    if (!chk.ok) {
      setErr(`권한 없음: ${chk.reason}`);
      return;
    }
    if (DIRECT[a.event]) {
      run(() => api.event(a.event, DIRECT[a.event].actor, DIRECT[a.event].payload));
    } else if (a.event === "E3_REPLY") {
      setTab("requests");
    } else {
      setFocus({ event: a.event, n: Date.now() });
    }
  };

  return (
    <div className="app">
      <header className="top">
        <div>
          <h1>C-201 입찰 · 기존 고객 약속 조율</h1>
          <p className="muted small">Corevia 영업·생산관리 W05 운영 서비스 (초안) · 부서별 권한 · 과거 기록으로 날짜를 골라 그날 기준으로 재현</p>
        </div>
        <div className="scenarios">
          <span className="muted small" title={`새 시뮬레이션 세션에서 돌린다 · 기준일 ${v.as_of}`}>시연</span>
          {v.scenarios.map((s) => (
            <button key={s.id} className="ghost" title={`${s.description} — 새 시뮬레이션 세션에서 돌린다(지금 세션은 그대로)`} disabled={busy || !dept} onClick={() => run(() => api.scenario(s.id, v.as_of))}>{s.title}</button>
          ))}
          <button className="ghost" disabled={busy || !canReset} title={canReset ? "지금 세션의 사건을 지우고 기준일부터 다시 시작한다" : "기준 세션 초기화는 운영 관리 부서만"} onClick={() => { if (window.confirm(`세션 "${v.session.name}"의 사건을 모두 지운다`)) run(() => api.reset(v.as_of)); }}>이 세션 초기화</button>
        </div>
      </header>
      <ContextBar v={v} run={run} busy={busy} dept={dept} roleCode={roleCode} onChoose={chooseRole} onCompare={() => setTab("sessions")} />
      {err && <div className="error" role="alert">{err}</div>}
      <div className="layout">
        <aside>
          <EventPanel v={v} run={run} focus={focus} dept={dept} />
          <TasksView v={v} />
        </aside>
        <main>
          <TabNav tab={tab} setTab={setTab} />
          {tab === "my" && <MyWorkView v={v} dept={dept} onAction={onAction} onChoose={chooseRole} onFocus={(event) => setFocus({ event, n: Date.now() })} goTab={setTab} />}
          {tab === "lanes" && <LanesView v={v} onAction={onAction} />}
          {tab === "inbox" && <InboxView v={v} />}
          {tab === "lines" && (<><LineBoardView v={v} /><SourceGapView v={v} /></>)}
          {tab === "judge" && <JudgeView v={v} dept={dept} onAction={onAction} onTrySim={trySim} />}
          {tab === "capacity" && (<><CapacityView v={v} /><CompareView v={v} /><SourceGapView v={v} /></>)}
          {tab === "ledger" && (<><LedgerView v={v} /><ChurnView v={v} /></>)}
          {tab === "requests" && (<><RequestsView v={v} run={run} dept={dept} /><DecisionsView v={v} /></>)}
          {tab === "log" && <LogView v={v} onFork={dept ? (seq) => run(() => api.fork(seq)) : undefined} busy={busy} />}
          {tab === "sim" && <SimulationView v={v} run={run} busy={busy} dept={dept} goTab={setTab} />}
          {tab === "sessions" && <SessionCompareView v={v} />}
          {tab === "backtest" && <BacktestView v={v} />}
          {tab === "assume" && <AssumptionsView v={v} />}
        </main>
      </div>
    </div>
  );
}
