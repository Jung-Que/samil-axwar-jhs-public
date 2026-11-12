import { useCallback, useEffect, useState } from "react";
import { ApiError, api, setSession } from "./api";
import type { LaneAction, View } from "./types";
import { SessionBar, SessionCompareView, SimulationView } from "./sessionViews";
import { AssumptionsView, CapacityView, ChurnView, CompareView, DecisionsView, JudgmentCard, LedgerView, LogView, ScheduleView, SourceGapView, TasksView } from "./panels";
import { EventPanel, RequestsView } from "./EventPanel";
import { AsOfBar, BacktestView, InboxView, LanesView, LineBoardView } from "./asofViews";

const TABS = [
  { id: "lanes", label: "업무 레인" },
  { id: "inbox", label: "접수함" },
  { id: "lines", label: "생산 라인" },
  { id: "judge", label: "판단·제안" },
  { id: "capacity", label: "여유·증설안" },
  { id: "ledger", label: "약속 원장" },
  { id: "requests", label: "확인 요청·결정" },
  { id: "log", label: "사건 로그" },
  { id: "sim", label: "시뮬레이션" },
  { id: "sessions", label: "세션 비교" },
  { id: "backtest", label: "정답 비교" },
  { id: "assume", label: "가정" },
] as const;
type Tab = (typeof TABS)[number]["id"];

// 바로 기록해도 되는 사건(입력 필드가 필요 없음). 나머지는 왼쪽 사건 입력으로 보낸다.
const DIRECT: Record<string, { actor: string; payload: unknown }> = {
  E8_SUBMIT_APPROVAL: { actor: "영업", payload: { note: "업무 레인 보드에서 상신" } },
  E7_SUBMIT_PROPOSAL: { actor: "영업", payload: {} },
};

// W05 운영 서비스 화면: 기준일을 고르고, 사건 입력 → 엔진 판단 → 사람 승인 → 제안·작업·상태 갱신.
export default function App() {
  const [v, setV] = useState<View | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [focus, setFocus] = useState<{ event: string; n: number } | null>(null);
  // 탭은 주소 해시(#inbox 등)로도 연다. 시연 중 특정 화면을 바로 보여 주기 위해서다.
  const fromHash = () => {
    const h = window.location.hash.slice(1);
    return (TABS.find((t) => t.id === h)?.id ?? "lanes") as Tab;
  };
  const [tab, setTabState] = useState<Tab>(fromHash);
  const setTab = (t: Tab) => {
    setTabState(t);
    window.history.replaceState(null, "", `#${t}`);
  };

  useEffect(() => {
    // 기억해 둔 세션이 지워졌으면(404) 기준 세션으로 연다.
    api
      .state()
      .catch((e: unknown) => {
        if (e instanceof ApiError && e.status === 404) {
          setSession("main");
          return api.state();
        }
        throw e;
      })
      .then(setV)
      .catch((e: Error) => setErr(`API 연결 실패: ${e.message}`));
  }, []);

  // 서버가 돌려준 세션을 지금 세션으로 쓴다(새 세션·분기·시연·세션 선택 뒤).
  useEffect(() => {
    if (v?.session?.id) setSession(v.session.id);
  }, [v?.session?.id]);

  const run = useCallback(async (fn: () => Promise<View>) => {
    setBusy(true);
    setErr(null);
    try {
      const nv = await fn();
      if (nv?.session?.id) setSession(nv.session.id); // 다음 호출부터 이 세션을 쓴다
      setV(nv);
    } catch (e) {
      if (e instanceof ApiError && e.view) setV(e.view);
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }, []);

  // 업무 레인 보드의 "할 수 있는 일"
  const onAction = (a: LaneAction) => {
    if (!a.enabled) return;
    if (DIRECT[a.event]) {
      run(() => api.event(a.event, DIRECT[a.event].actor, DIRECT[a.event].payload));
    } else if (a.event === "E3_REPLY") {
      setTab("requests");
    } else {
      setFocus({ event: a.event, n: Date.now() });
    }
  };

  if (!v) return <main className="center">{err ?? "불러오는 중…"}</main>;
  const bid = v.state.bid;

  return (
    <div className="app">
      <header className="top">
        <div>
          <h1>C-201 입찰 · 기존 고객 약속 조율</h1>
          <p className="muted small">Corevia 영업·생산관리 W05 운영 서비스 (초안) · 과거 기록으로 날짜를 골라 그날 기준으로 재현</p>
        </div>
        <div className="status">
          <span className="chip strong">입찰 {bid ? `${bid.state} · v${bid.version}` : "접수 전"}</span>
          <span className="chip">저장소 {v.store}</span>
          <span className="chip">입력 {v.input_mode}</span>
          <span className="chip">원본 {v.raw_files_verified}개 해시 확인</span>
        </div>
        <div className="scenarios">
          <span className="muted small">시연(새 시뮬레이션 세션 · 기준일 {v.as_of})</span>
          {v.scenarios.map((s) => (
            <button key={s.id} title={`${s.description} — 새 시뮬레이션 세션에서 돌린다(지금 세션은 그대로)`} disabled={busy} onClick={() => run(() => api.scenario(s.id, v.as_of))}>{s.title}</button>
          ))}
          <button className="ghost" disabled={busy} title="지금 세션의 사건을 지우고 기준일부터 다시 시작한다" onClick={() => { if (window.confirm(`세션 "${v.session.name}"의 사건을 모두 지운다`)) run(() => api.reset(v.as_of)); }}>이 세션 초기화</button>
        </div>
      </header>
      <SessionBar v={v} run={run} busy={busy} onCompare={() => setTab("sessions")} />
      <AsOfBar v={v} run={run} busy={busy} />
      {err && <div className="error" role="alert">{err}</div>}
      <div className="layout">
        <aside>
          <EventPanel v={v} run={run} focus={focus} />
          <TasksView v={v} />
        </aside>
        <main>
          <nav className="tabs">
            {TABS.map((t) => (
              <button key={t.id} className={tab === t.id ? "tab active" : "tab"} onClick={() => setTab(t.id)}>{t.label}</button>
            ))}
          </nav>
          {tab === "lanes" && <LanesView v={v} onAction={onAction} />}
          {tab === "inbox" && <InboxView v={v} />}
          {tab === "lines" && (<><LineBoardView v={v} /><SourceGapView v={v} /></>)}
          {tab === "judge" && (<><JudgmentCard v={v} /><ScheduleView v={v} /><CompareView v={v} /></>)}
          {tab === "capacity" && (<><CapacityView v={v} /><CompareView v={v} /><SourceGapView v={v} /></>)}
          {tab === "ledger" && (<><LedgerView v={v} /><ChurnView v={v} /></>)}
          {tab === "requests" && (<><RequestsView v={v} run={run} /><DecisionsView v={v} /></>)}
          {tab === "log" && <LogView v={v} onFork={(seq) => run(() => api.fork(seq))} busy={busy} />}
          {tab === "sim" && <SimulationView v={v} run={run} busy={busy} />}
          {tab === "sessions" && <SessionCompareView v={v} />}
          {tab === "backtest" && <BacktestView v={v} />}
          {tab === "assume" && <AssumptionsView v={v} />}
        </main>
      </div>
    </div>
  );
}
