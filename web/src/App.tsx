import { useCallback, useEffect, useState } from "react";
import { ApiError, api } from "./api";
import type { View } from "./types";
import { AssumptionsView, CapacityView, DecisionsView, JudgmentCard, LedgerView, LogView, ScheduleView, TasksView } from "./panels";
import { EventPanel, RequestsView } from "./EventPanel";

const TABS = [
  { id: "judge", label: "판단·제안" },
  { id: "capacity", label: "라인·증설안" },
  { id: "ledger", label: "약속 원장" },
  { id: "requests", label: "확인 요청·결정" },
  { id: "log", label: "사건 로그" },
  { id: "assume", label: "가정" },
] as const;
type Tab = (typeof TABS)[number]["id"];

// W05 운영 서비스 화면: 사건 입력 → 엔진 판단 → 사람 승인 → 제안·작업·상태 갱신.
export default function App() {
  const [v, setV] = useState<View | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  // 탭은 주소 해시(#capacity 등)로도 연다. 시연 중 특정 화면을 바로 보여 주기 위해서다.
  const fromHash = () => {
    const h = window.location.hash.slice(1);
    return (TABS.find((t) => t.id === h)?.id ?? "judge") as Tab;
  };
  const [tab, setTabState] = useState<Tab>(fromHash);
  const setTab = (t: Tab) => {
    setTabState(t);
    window.history.replaceState(null, "", `#${t}`);
  };

  useEffect(() => {
    api.state().then(setV).catch((e: Error) => setErr(`API 연결 실패: ${e.message}`));
  }, []);

  const run = useCallback(async (fn: () => Promise<View>) => {
    setBusy(true);
    setErr(null);
    try {
      setV(await fn());
    } catch (e) {
      if (e instanceof ApiError && e.view) setV(e.view);
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }, []);

  if (!v) return <main className="center">{err ?? "불러오는 중…"}</main>;
  const bid = v.state.bid;

  return (
    <div className="app">
      <header className="top">
        <div>
          <h1>C-201 입찰 · 기존 고객 약속 조율</h1>
          <p className="muted small">Corevia 영업·생산관리 W05 운영 서비스 (초안) · 기준일 {v.as_of} · 이사회 {v.board_date}</p>
        </div>
        <div className="status">
          <span className="chip strong">입찰 {bid ? `${bid.state} · v${bid.version}` : "접수 전"}</span>
          <span className="chip">업무일 {v.state.scenario.sim_date}</span>
          <span className="chip">저장소 {v.store}</span>
          <span className={`chip ai-${v.ai_mode}`}>{v.ai_mode === "live" ? `AI 실제 호출 · ${v.ai_model}` : "AI 키 없음 · 녹화 응답 [모의]"}</span>
          <span className="chip">원본 {v.raw_files_verified}개 해시 확인</span>
        </div>
        <div className="scenarios">
          <span className="muted small">시연</span>
          {v.scenarios.map((s) => (
            <button key={s.id} title={s.description} disabled={busy} onClick={() => run(() => api.scenario(s.id))}>{s.title}</button>
          ))}
          <button className="ghost" disabled={busy} onClick={() => run(api.reset)}>초기화</button>
        </div>
      </header>
      {err && <div className="error" role="alert">{err}</div>}
      <div className="layout">
        <aside>
          <EventPanel v={v} run={run} />
          <TasksView v={v} />
        </aside>
        <main>
          <nav className="tabs">
            {TABS.map((t) => (
              <button key={t.id} className={tab === t.id ? "tab active" : "tab"} onClick={() => setTab(t.id)}>{t.label}</button>
            ))}
          </nav>
          {tab === "judge" && (<><JudgmentCard v={v} /><ScheduleView v={v} /></>)}
          {tab === "capacity" && <CapacityView v={v} />}
          {tab === "ledger" && <LedgerView v={v} />}
          {tab === "requests" && (<><RequestsView v={v} run={run} /><DecisionsView v={v} /></>)}
          {tab === "log" && <LogView v={v} />}
          {tab === "assume" && <AssumptionsView v={v} />}
        </main>
      </div>
    </div>
  );
}
