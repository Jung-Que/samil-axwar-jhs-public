import { useEffect, useState } from "react";
import { api } from "./api";
import type { ItemDiff, MonthDiff, SessionCompare, SessionInfo, SessionKPI, View } from "./types";
import { Section } from "./panels";
import { PRODUCT_LABEL, ea, eok, pct } from "./format";

type Run = (fn: () => Promise<View>) => Promise<void>;

const kindLabel = (s: SessionInfo) => (s.kind === "main" ? "기준" : "시뮬레이션");
const sessionLabel = (s: SessionInfo) => `${s.name} · ${kindLabel(s)}${s.events >= 0 ? ` · 사건 ${s.events}` : ""}`;

// 세션 막대(이슈 #37): 세션 고르기·새 시뮬레이션·지금 상태에서 분기·삭제. 기준 세션(main)은 나중의 운영 세션이다.
export function SessionBar({ v, run, busy, onCompare }: { v: View; run: Run; busy: boolean; onCompare: () => void }) {
  const cur = v.session;
  const list = v.sessions ?? [cur];
  const events = v.state.log?.length ?? 0;
  const parent = list.find((s) => s.id === cur.parent_id);
  return (
    <div className={`sessionbar ${cur.kind === "sim" ? "is-sim" : ""}`}>
      <label className="field inline">
        <span>세션</span>
        <select value={cur.id} disabled={busy} onChange={(e) => run(() => api.open(e.target.value))}>
          {list.map((s) => (
            <option key={s.id} value={s.id}>{sessionLabel(s)}</option>
          ))}
        </select>
      </label>
      <span className={`chip ${cur.kind === "sim" ? "warn" : "strong"}`}>{cur.kind === "sim" ? "시뮬레이션 세션 · 제출은 [모의]" : "기준 세션(이후 운영 세션)"}</span>
      {cur.parent_id && <span className="muted small">분기: {parent?.name ?? cur.parent_id} 사건 {cur.fork_seq}까지</span>}
      <button className="ghost small" disabled={busy} onClick={() => run(() => api.newSession("", v.as_of))} title="지금 세션 기준일로 빈 시뮬레이션 세션을 만든다">새 시뮬레이션</button>
      <button className="ghost small" disabled={busy} onClick={() => run(() => api.fork(events))} title="지금 상태(사건 전체)를 복사한 시뮬레이션 세션을 만든다">지금 상태에서 분기</button>
      <button className="ghost small" disabled={busy || list.length < 2} onClick={onCompare}>세션 비교</button>
      {cur.kind === "sim" && (
        <button className="ghost small danger" disabled={busy} onClick={() => { if (window.confirm(`세션 "${cur.name}"을 지운다`)) run(() => api.deleteSession(cur.id)); }}>
          세션 삭제
        </button>
      )}
    </div>
  );
}

// 세션 비교(S2): 두 세션의 KPI·제품×월 공급/제안/부족·플래그·확인 요청·작업 차이.
export function SessionCompareView({ v }: { v: View }) {
  const list = v.sessions ?? [];
  const cur = v.session;
  const defA = cur.parent_id && list.some((s) => s.id === cur.parent_id) ? cur.parent_id : list.find((s) => s.id !== cur.id)?.id ?? cur.id;
  const [a, setA] = useState(defA);
  const [b, setB] = useState(cur.id);
  const [c, setC] = useState<SessionCompare | null>(null);
  const [err, setErr] = useState<string | null>(null);
  useEffect(() => {
    if (!a || !b) return;
    setErr(null);
    api.compare(a, b).then(setC).catch((e: Error) => setErr(e.message));
  }, [a, b, v]);
  return (
    <Section title="세션 비교" mode={["real"]} note="두 세션을 같은 표로 본다. 시뮬레이션 세션의 결과는 모의다. 달라진 칸은 강조한다.">
      <div className="row wrap">
        <label className="field inline"><span>A</span>
          <select value={a} onChange={(e) => setA(e.target.value)}>{list.map((s) => <option key={s.id} value={s.id}>{sessionLabel(s)}</option>)}</select>
        </label>
        <label className="field inline"><span>B</span>
          <select value={b} onChange={(e) => setB(e.target.value)}>{list.map((s) => <option key={s.id} value={s.id}>{sessionLabel(s)}</option>)}</select>
        </label>
      </div>
      {err && <p className="bad small">{err}</p>}
      {c && (
        <>
          <KpiTable a={c.a} b={c.b} />
          {["PL-02", "PL-01"].map((p) => <MonthTable key={p} product={p} rows={c.products[p] ?? []} />)}
          <DiffList title="미확인·위험·차단 플래그" rows={c.flags ?? []} />
          <DiffList title="확인 요청 상태" rows={c.requests ?? []} />
          <DiffList title="작업 상태" rows={c.tasks ?? []} />
        </>
      )}
    </Section>
  );
}

function KpiTable({ a, b }: { a: SessionKPI; b: SessionKPI }) {
  const rows: [string, string, string][] = [
    ["세션", `${a.session.name} (${kindLabel(a.session)})`, `${b.session.name} (${kindLabel(b.session)})`],
    ["기준일 / 업무일", `${a.as_of} / ${a.sim_date}`, `${b.as_of} / ${b.sim_date}`],
    ["입찰 상태", `${a.bid_state}${a.bid_version ? ` · v${a.bid_version}` : ""}`, `${b.bid_state}${b.bid_version ? ` · v${b.bid_version}` : ""}`],
    ["단가 인하 / 납품 개시", `${pct(a.price_cut * 100)} / ${a.delivery_start}`, `${pct(b.price_cut * 100)} / ${b.delivery_start}`],
    ["PL-01 제안 (상태)", `${ea(a.pl01_schedule)} EA (${a.pl01_status || "-"})`, `${ea(b.pl01_schedule)} EA (${b.pl01_status || "-"})`],
    ["PL-02 제안 (공급률)", `${ea(a.pl02_schedule)} EA (${pct(a.pl02_coverage_pct)})`, `${ea(b.pl02_schedule)} EA (${pct(b.pl02_coverage_pct)})`],
    ["전량 약속 시 지체상금(지연분)", eok(a.penalty_backlog_krw), eok(b.penalty_backlog_krw)],
    ["계약 총액 기준 지체상금", eok(a.penalty_contract_krw), eok(b.penalty_contract_krw)],
    ["제안 기준 매출 / 공헌이익", `${eok(a.revenue_schedule_krw, 1)} / ${eok(a.contrib_schedule_krw, 1)}`, `${eok(b.revenue_schedule_krw, 1)} / ${eok(b.contrib_schedule_krw, 1)}`],
    ["증설·전환안", (a.options ?? []).join(" | "), (b.options ?? []).join(" | ")],
    ["열린 확인 요청 / 작업 / 가예약", `${a.open_requests} / ${a.open_tasks} / ${a.reservations}`, `${b.open_requests} / ${b.open_tasks} / ${b.reservations}`],
  ];
  return (
    <table className="grid text">
      <thead><tr><th>항목</th><th>A</th><th>B</th></tr></thead>
      <tbody>
        {rows.map(([k, x, y]) => (
          <tr key={k}><td>{k}</td><td>{x}</td><td className={x !== y && k !== "세션" ? "warn" : ""}>{y}</td></tr>
        ))}
      </tbody>
    </table>
  );
}

function MonthTable({ product, rows }: { product: string; rows: MonthDiff[] }) {
  if (rows.length === 0) return null;
  // 계약 기간 밖의 달은 "-"(0과 구분). 한쪽에만 있는 달도 강조한다.
  const val = (has: boolean, x: number) => (has ? ea(x) : "-");
  const cell = (m: MonthDiff, x: number, y: number) => (m.a_has !== m.b_has || x !== y ? "warn" : "");
  const row = (label: string, cls: string, pick: (m: MonthDiff) => [number, number]) => (
    <tr className={cls}>
      <td>{label}</td>
      {rows.map((m) => {
        const [x, y] = pick(m);
        return <td key={m.month} className={cell(m, x, y)}>{val(m.a_has, x)} / {val(m.b_has, y)}</td>;
      })}
    </tr>
  );
  return (
    <div className="scroll">
      <table className="grid">
        <thead>
          <tr><th>{PRODUCT_LABEL[product] ?? product} 월</th>{rows.map((m) => <th key={m.month}>{m.month.slice(2)}</th>)}</tr>
        </thead>
        <tbody>
          {row("공급 A / B", "", (m) => [m.a_supply, m.b_supply])}
          {row("제안 A / B", "row-strong", (m) => [m.a_schedule, m.b_schedule])}
          {row("부족 A / B", "row-bad", (m) => [m.a_short, m.b_short])}
        </tbody>
      </table>
    </div>
  );
}

function DiffList({ title, rows }: { title: string; rows: ItemDiff[] }) {
  return (
    <>
      <h3>{title} 차이 ({rows.length})</h3>
      {rows.length === 0 ? (
        <p className="muted small">차이 없음</p>
      ) : (
        <table className="grid text">
          <thead><tr><th>항목</th><th>A</th><th>B</th></tr></thead>
          <tbody>
            {rows.map((r) => <tr key={r.key}><td className="small">{r.text}</td><td>{r.a || "없음"}</td><td className="warn">{r.b || "없음"}</td></tr>)}
          </tbody>
        </table>
      )}
    </>
  );
}
