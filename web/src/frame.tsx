import { useEffect, useState } from "react";
import { api } from "./api";
import type { Department, SessionInfo, View } from "./types";
import { canFeature } from "./perm";

type Run = (fn: () => Promise<View>) => Promise<void>;

// 앱 틀(이슈 #47): 부서·세션·시점·입찰을 맥락 막대 하나로 보이고, 탭 13개를 4묶음으로 나눈다.

export const TAB_GROUPS = [
  {
    id: "work",
    label: "업무",
    tabs: [
      { id: "my", label: "내 업무" },
      { id: "requests", label: "확인 요청·결정" },
      { id: "log", label: "사건 로그" },
      { id: "lanes", label: "업무 레인" },
      { id: "inbox", label: "접수함" },
    ],
  },
  {
    id: "judge",
    label: "판단",
    tabs: [
      { id: "judge", label: "판단·제안" },
      { id: "capacity", label: "여유·증설안" },
      { id: "ledger", label: "약속 원장" },
      { id: "lines", label: "생산 라인" },
    ],
  },
  {
    id: "sim",
    label: "시뮬레이션",
    tabs: [
      { id: "sim", label: "시뮬레이션" },
      { id: "sessions", label: "세션 비교" },
    ],
  },
  {
    id: "basis",
    label: "근거",
    tabs: [
      { id: "assume", label: "가정" },
      { id: "backtest", label: "정답 비교" },
    ],
  },
] as const;

export type Tab = (typeof TAB_GROUPS)[number]["tabs"][number]["id"];
export const ALL_TABS: Tab[] = TAB_GROUPS.flatMap((g) => g.tabs.map((t) => t.id));

const groupOf = (t: Tab) => TAB_GROUPS.find((g) => g.tabs.some((x) => x.id === t)) ?? TAB_GROUPS[0];

export function TabNav({ tab, setTab }: { tab: Tab; setTab: (t: Tab) => void }) {
  const cur = groupOf(tab);
  return (
    <nav className="tabnav" aria-label="화면">
      <div className="tabgroups">
        {TAB_GROUPS.map((g) => (
          <button key={g.id} className={g.id === cur.id ? "tabgroup active" : "tabgroup"} aria-pressed={g.id === cur.id} onClick={() => setTab(g.tabs[0].id)}>
            {g.label}
          </button>
        ))}
      </div>
      <div className="tabs">
        {cur.tabs.map((t) => (
          <button key={t.id} className={tab === t.id ? "tab active" : "tab"} aria-current={tab === t.id ? "page" : undefined} onClick={() => setTab(t.id)}>
            {t.label}
          </button>
        ))}
      </div>
    </nav>
  );
}

// 입찰 상태 표시(팔레트, 이슈 #51): 진행 = 검정 채움 · 승인·수주 = 회색 채움 · 조건부·재검토·보류 = 빨강 테두리 · 실주 = 빨강 채움.
export function bidTone(state?: string): string {
  if (!state || state === "검토중") return "tone-gray";
  if (state === "승인" || state === "수주") return "tone-ok";
  if (state === "조건부 승인" || state === "재검토" || state === "보류") return "tone-warn";
  if (state === "실주") return "tone-bad";
  return "tone-accent";
}

const PRESETS = ["2024-12-31", "2025-06-30", "2025-12-31", "2026-03-31", "2026-06-30", "2026-07-01"];

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

const kindLabel = (s: SessionInfo) => (s.kind === "main" ? "기준" : "시뮬레이션");
const sessionLabel = (s: SessionInfo) => `${s.name} · ${kindLabel(s)}${s.events >= 0 ? ` · 사건 ${s.events}` : ""}`;

type BarProps = { v: View; run: Run; busy: boolean; dept?: Department; roleCode: string; onChoose: (code: string) => void; onCompare: () => void };

export function ContextBar(p: BarProps) {
  const sim = p.v.session.kind === "sim";
  return (
    <section className={`context ${sim ? "is-sim" : ""}`} aria-label="지금 맥락">
      <RoleCell {...p} />
      <SessionCell {...p} />
      <TimeCell {...p} />
      <BidCell v={p.v} />
    </section>
  );
}

// 내 부서: 시연용 부서 선택(롤 체크, 로그인·인증 없음). 권한은 부서별이고 서버가 최종 판단한다.
function RoleCell({ v, dept, roleCode, onChoose }: BarProps) {
  const list = v.departments ?? [];
  const groups = [...new Set(list.map((d) => d.dept))];
  return (
    <div className={`ctx-cell ctx-role ${dept ? "" : "no-role"}`}>
      <label className="ctx-label" htmlFor="ctx-dept">내 부서</label>
      <div className="row">
        <select id="ctx-dept" value={roleCode} onChange={(e) => onChoose(e.target.value)}>
          <option value="">부서를 고른다</option>
          {groups.map((g) => (
            <optgroup key={g} label={g}>
              {list.filter((d) => d.dept === g).map((d) => <option key={d.code} value={d.code}>{d.label}</option>)}
            </optgroup>
          ))}
        </select>
      </div>
      {dept ? <span className="small">{dept.duty}</span> : <span className="small bad">부서를 고르면 그 부서 권한으로 입력할 수 있다(조회는 그대로)</span>}
      <span className="muted tiny">시연용 롤 체크 · 로그인·인증 없음</span>
    </div>
  );
}

// 세션: 고르기·새 시뮬레이션·지금 상태에서 분기·비교·삭제. 기준 세션(main)은 나중의 운영 세션이다.
function SessionCell({ v, run, busy, dept, onCompare }: BarProps) {
  const cur = v.session;
  const list = v.sessions ?? [cur];
  const events = v.state.log?.length ?? 0;
  const parent = list.find((s) => s.id === cur.parent_id);
  const canMake = canFeature(dept, "sim.session");
  const canDrop = canFeature(dept, "session.delete");
  return (
    <div className="ctx-cell ctx-session">
      <label className="ctx-label" htmlFor="ctx-session">세션</label>
      <div className="row">
        <select id="ctx-session" value={cur.id} disabled={busy} onChange={(e) => run(() => api.open(e.target.value))}>
          {list.map((s) => (
            <option key={s.id} value={s.id}>{sessionLabel(s)}</option>
          ))}
        </select>
      </div>
      <span className="small">
        {cur.kind === "sim" ? <b className="warn-text">시뮬레이션 · 제출은 [모의]</b> : <b>기준 세션 · 자기 부서 권한으로만 입력</b>}
        {cur.parent_id && <span className="muted"> · 분기: {parent?.name ?? cur.parent_id} 사건 {cur.fork_seq}까지</span>}
      </span>
      <div className="row">
        <button className="ghost small" disabled={busy || !canMake} onClick={() => run(() => api.newSession("", v.as_of))} title={canMake ? "지금 세션 기준일로 빈 시뮬레이션 세션을 만든다" : "부서를 먼저 고른다"}>새 시뮬레이션</button>
        <button className="ghost small" disabled={busy || !canMake} onClick={() => run(() => api.fork(events))} title={canMake ? "지금 상태(사건 전체)를 복사한 시뮬레이션 세션을 만든다" : "부서를 먼저 고른다"}>분기</button>
        <button className="ghost small" disabled={busy || list.length < 2} onClick={onCompare}>비교</button>
        {cur.kind === "sim" && (
          <button className="ghost small danger" disabled={busy || !canDrop} title={canDrop ? "" : "세션 삭제는 운영 관리 부서만"} onClick={() => { if (window.confirm(`세션 "${cur.name}"을 지운다`)) run(() => api.deleteSession(cur.id)); }}>
            삭제
          </button>
        )}
      </div>
    </div>
  );
}

// 시점: 세션 기준일·업무일·쓰는 기록·이사회일과 시계(E0). 다른 기준일로 시작하기는 접어 둔다.
function TimeCell({ v, run, busy, dept }: BarProps) {
  const [pick, setPick] = useState(v.as_of);
  useEffect(() => setPick(v.as_of), [v.as_of]);
  const sim = v.state.scenario.sim_date;
  // 시계: 기준 세션은 운영 관리만, 시뮬레이션 세션은 모든 부서(시스템 대행)
  const canClock = v.session.kind === "main" ? canFeature(dept, "main.clock") : canFeature(dept, "sim.act");
  const clockWhy = canClock ? "" : v.session.kind === "main" ? "기준 세션 시계는 운영 관리 부서만" : "부서를 먼저 고른다";
  const clock = (to: string) => run(() => api.event("E0_CLOCK", "시스템", { sim_date: to }));
  return (
    <div className="ctx-cell ctx-time">
      <span className="ctx-label">시점</span>
      <span className="small">
        기준일 <b>{v.as_of}</b> · 업무일 <b>{sim}</b>
      </span>
      <span className="muted small">
        쓰는 기록 ~{v.cutoff} · 이사회 {v.board_date}
        {v.state.evaluation.future_snapshot && <span className="warn-text" title="능력 스냅샷은 자료 마지막 날(2026-06-30) 것 하나뿐이다(T2)"> · 미래 자료: 능력 스냅샷 {v.data_end}</span>}
      </span>
      <div className="row">
        <span className="muted small">시계</span>
        <button className="ghost small" disabled={busy || !canClock} title={clockWhy} onClick={() => clock(addDays(sim, 1))}>+1일</button>
        <button className="ghost small" disabled={busy || !canClock} title={clockWhy} onClick={() => clock(addDays(sim, 7))}>+1주</button>
        <button className="ghost small" disabled={busy || !canClock} title={clockWhy} onClick={() => clock(addMonths(sim, 1))}>+1달</button>
      </div>
      <details className="ctx-more">
        <summary className="small">다른 기준일로 새 세션</summary>
        <div className="row">
          <input type="date" aria-label="기준일" min={v.as_of_min} max={v.as_of_max} value={pick} onChange={(e) => setPick(e.target.value)} />
          <button className="small" disabled={busy || !pick || !dept} title={dept ? "그 날짜까지의 기록으로 새 시뮬레이션 세션을 만든다(지금 세션은 그대로)" : "부서를 먼저 고른다"} onClick={() => run(() => api.newSession("", pick))}>
            시작
          </button>
        </div>
        <div className="row">
          {PRESETS.map((d) => (
            <button key={d} className={d === pick ? "ghost small active" : "ghost small"} disabled={busy} onClick={() => setPick(d)}>
              {d}
            </button>
          ))}
        </div>
      </details>
    </div>
  );
}

// 입찰: 상태와 이 화면이 실제로 도는 근거(저장소·원본 해시·입력 방식).
function BidCell({ v }: { v: View }) {
  const bid = v.state.bid;
  return (
    <div className="ctx-cell ctx-bid">
      <span className="ctx-label">입찰 {bid?.bid_id ?? "C-201"}</span>
      <div className="row">
        <span className={`pill strong ${bidTone(bid?.state)}`}>{bid ? `${bid.state} · v${bid.version}` : "접수 전"}</span>
        {bid && <span className="muted small">마감 {bid.deadline}</span>}
      </div>
      <span className="muted tiny">
        저장소 {v.store} · 원본 {v.raw_files_verified}개 해시 확인 · 입력 {v.input_mode}
      </span>
    </div>
  );
}
