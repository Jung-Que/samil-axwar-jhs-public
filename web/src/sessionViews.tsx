import { useEffect, useState } from "react";
import { api } from "./api";
import type { ItemDiff, MonthDiff, SessionCompare, SessionInfo, SessionKPI, SweepResult, View } from "./types";
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

// ---- 시뮬레이션 탭(S3, 이슈 #39): 모의 가정 사건(E9)과 민감도 격자 ----

const PARAMS: { key: string; label: string; kind: "num" | "str" | "bool" }[] = [
  { key: "price_cut_pct", label: "단가 인하(%)", kind: "num" },
  { key: "delivery_start", label: "납품 개시월(YYYY-MM)", kind: "str" },
  { key: "fx_shift", label: "환율 충격(원)", kind: "num" },
  { key: "contract_months", label: "계약 기간(개월)", kind: "num" },
  { key: "validation_weeks_opt01", label: "OPT-01 적격 주수", kind: "num" },
  { key: "validation_weeks_opt02", label: "OPT-02 적격 주수", kind: "num" },
  { key: "capacity_period", label: "능력 단위(annual·monthly)", kind: "str" },
  { key: "masil_in_util", label: "Masil 가동률 포함(true·false)", kind: "bool" },
  { key: "opt01_status", label: "OPT-01 결정(제안·승인·보류·거절)", kind: "str" },
  { key: "opt02_status", label: "OPT-02 결정(제안·승인·보류·거절)", kind: "str" },
];
const GOOD = new Set(["pl02_coverage_pct", "pl01_schedule", "revenue_schedule_krw", "contrib_schedule_krw"]);

function addMonth(ym: string, n: number): string {
  const [y, m] = ym.split("-").map(Number);
  const t = new Date(Date.UTC(y, m - 1 + n, 1));
  return `${t.getUTCFullYear()}-${String(t.getUTCMonth() + 1).padStart(2, "0")}`;
}

function parseValues(kind: string, text: string): unknown[] {
  return text
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean)
    .map((s) => (kind === "num" ? Number(s) : kind === "bool" ? s === "true" : s));
}

function fmtMetric(unit: string, v: number | undefined): string {
  if (v === undefined) return "-";
  if (unit === "%") return pct(v);
  if (unit === "원") return eok(v, 1);
  return ea(Math.round(v));
}

export function SimulationView({ v, run, busy }: { v: View; run: Run; busy: boolean }) {
  const sim = v.session.kind === "sim";
  const sc = v.state.scenario;
  const [f, setF] = useState<Record<string, string>>({});
  const set = (k: string, val: string) => setF({ ...f, [k]: val });
  const submit = () => {
    const p: Record<string, unknown> = {};
    if (f.delivery_start) p.delivery_start = f.delivery_start;
    if (f.contract_months) p.contract_months = Number(f.contract_months);
    if (f.price_cut_pct) p.price_cut_pct = Number(f.price_cut_pct);
    if (f.fx_shift) p.fx_shift = Number(f.fx_shift);
    if (f.capacity_period) p.capacity_period = f.capacity_period;
    if (f.masil_in_util) p.masil_in_util = f.masil_in_util === "true";
    const vw: Record<string, number> = {};
    if (f.vw01) vw["OPT-01"] = Number(f.vw01);
    if (f.vw02) vw["OPT-02"] = Number(f.vw02);
    if (Object.keys(vw).length) p.validation_weeks = vw;
    const op: Record<string, string> = {};
    if (f.op01) op["OPT-01"] = f.op01;
    if (f.op02) op["OPT-02"] = f.op02;
    if (Object.keys(op).length) p.options = op;
    run(() => api.event("E9_SIM_ASSUMPTION", "시뮬레이터", p)).then(() => setF({}));
  };

  const [xp, setXp] = useState("price_cut_pct");
  const [xv, setXv] = useState("0, -3, -5, -7, -10");
  const [yp, setYp] = useState("delivery_start");
  const ds = sc.delivery_start || "2026-10";
  const [yv, setYv] = useState([0, 2, 3, 6].map((n) => addMonth(ds, n)).join(", "));
  const [metric, setMetric] = useState("pl02_coverage_pct");
  const [res, setRes] = useState<SweepResult | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const kindOf = (k: string) => PARAMS.find((p) => p.key === k)?.kind ?? "str";
  const sweep = () => {
    setErr(null);
    api
      .sweep({ param: xp, values: parseValues(kindOf(xp), xv) }, yp ? { param: yp, values: parseValues(kindOf(yp), yv) } : undefined)
      .then(setRes)
      .catch((e: Error) => setErr(e.message));
  };
  // 탭을 열거나 세션·상태가 바뀌면 지금 입력값으로 한 번 계산해 둔다.
  useEffect(sweep, [v.session.id, v.state.log?.length]); // eslint-disable-line react-hooks/exhaustive-deps
  const m = res?.metrics.find((x) => x.key === metric);
  const all = res ? res.cells.flat().map((c) => c[metric]) : [];
  const lo = Math.min(...all);
  const hi = Math.max(...all);
  const heat = (val: number) => {
    if (!(hi > lo)) return undefined;
    const t = (val - lo) / (hi - lo);
    const good = GOOD.has(metric) ? t : 1 - t;
    return { background: `color-mix(in srgb, var(--ok) ${Math.round(good * 40)}%, color-mix(in srgb, var(--bad) ${Math.round((1 - good) * 30)}%, transparent))` };
  };

  return (
    <>
      <Section
        title="모의 가정 (E9)"
        mode={["real", "mock"]}
        note="시뮬레이션 세션에서만 넣는다. 원본 제약은 그대로 두고 이 세션의 평가만 바꾼다(A05 §6 대안 시나리오). 빈 칸은 바꾸지 않는다. 증설안 결정은 실제 이사회 결정이 아니다."
      >
        {!sim ? (
          <p className="blocked">
            지금은 기준 세션이다. 모의 가정은 넣지 않는다.{" "}
            <button className="ghost small" disabled={busy} onClick={() => run(() => api.fork(v.state.log?.length ?? 0, `${v.session.name} 모의 가정`))}>지금 상태에서 분기해 시작</button>
          </p>
        ) : (
          <>
            <div className="row wrap">
              <label className="field"><span>납품 개시월 (지금 {sc.delivery_start})</span><input placeholder="YYYY-MM" value={f.delivery_start ?? ""} onChange={(e) => set("delivery_start", e.target.value)} /></label>
              <label className="field"><span>계약 기간 (지금 {sc.contract_months}개월)</span><input type="number" value={f.contract_months ?? ""} onChange={(e) => set("contract_months", e.target.value)} /></label>
              <label className="field"><span>단가 인하 % (지금 {pct(sc.price_cut * 100)})</span><input type="number" placeholder="-7" value={f.price_cut_pct ?? ""} onChange={(e) => set("price_cut_pct", e.target.value)} /></label>
              <label className="field"><span>환율 충격 원 (지금 {sc.fx_shift ?? 0})</span><input type="number" placeholder="-100" value={f.fx_shift ?? ""} onChange={(e) => set("fx_shift", e.target.value)} /></label>
              <label className="field"><span>능력 단위 (지금 {sc.capacity_period})</span>
                <select value={f.capacity_period ?? ""} onChange={(e) => set("capacity_period", e.target.value)}><option value="">그대로</option><option value="annual">연간</option><option value="monthly">월간</option></select>
              </label>
              <label className="field"><span>Masil 가동률 포함 (지금 {sc.masil_in_util ? "포함" : "미포함"})</span>
                <select value={f.masil_in_util ?? ""} onChange={(e) => set("masil_in_util", e.target.value)}><option value="">그대로</option><option value="false">미포함(먼저 뺌)</option><option value="true">포함</option></select>
              </label>
              <label className="field"><span>OPT-01 적격 주수</span><input type="number" value={f.vw01 ?? ""} onChange={(e) => set("vw01", e.target.value)} /></label>
              <label className="field"><span>OPT-02 적격 주수</span><input type="number" value={f.vw02 ?? ""} onChange={(e) => set("vw02", e.target.value)} /></label>
              {(["op01", "op02"] as const).map((k) => (
                <label key={k} className="field"><span>{k === "op01" ? "OPT-01" : "OPT-02"} 결정(모의)</span>
                  <select value={f[k] ?? ""} onChange={(e) => set(k, e.target.value)}><option value="">그대로</option>{["제안", "승인", "보류", "거절"].map((s) => <option key={s} value={s}>{s}</option>)}</select>
                </label>
              ))}
            </div>
            <button disabled={busy || Object.values(f).every((x) => !x)} onClick={submit}>모의 가정 넣기(E9)</button>
          </>
        )}
        <h3>이 세션에 적용된 모의 가정 ({sc.sim_notes?.length ?? 0})</h3>
        {(sc.sim_notes ?? []).length === 0 ? <p className="muted small">없음</p> : <ul className="conds">{(sc.sim_notes ?? []).map((n, i) => <li key={i}>{n}</li>)}</ul>}
      </Section>

      <Section title="민감도 격자" mode={["real"]} note="변수 1~2개를 골라 값을 쉼표로 적으면, 지금 세션 시나리오에서 그 값만 바꿔 엔진 평가를 반복한다. 사건을 남기지 않아 기준 세션에서도 된다(최대 400칸).">
        <div className="row wrap">
          <label className="field"><span>X 변수</span><select value={xp} onChange={(e) => setXp(e.target.value)}>{PARAMS.map((p) => <option key={p.key} value={p.key}>{p.label}</option>)}</select></label>
          <label className="field wide"><span>X 값(쉼표)</span><input value={xv} onChange={(e) => setXv(e.target.value)} /></label>
          <label className="field"><span>Y 변수</span><select value={yp} onChange={(e) => setYp(e.target.value)}><option value="">없음</option>{PARAMS.filter((p) => p.key !== xp).map((p) => <option key={p.key} value={p.key}>{p.label}</option>)}</select></label>
          {yp && <label className="field wide"><span>Y 값(쉼표)</span><input value={yv} onChange={(e) => setYv(e.target.value)} /></label>}
          <label className="field"><span>지표</span><select value={metric} onChange={(e) => setMetric(e.target.value)}>{(res?.metrics ?? DEFAULT_METRICS).map((x) => <option key={x.key} value={x.key}>{x.label}</option>)}</select></label>
        </div>
        <button disabled={busy} onClick={sweep}>격자 계산</button>
        {err && <p className="bad small">{err}</p>}
        {res && m && (
          <div className="scroll">
            <p className="muted small">지금 세션 그대로: {m.label} {fmtMetric(m.unit, res.base[metric])}. 진할수록 {GOOD.has(metric) ? "높음(좋음)" : "낮음(좋음)"}.</p>
            <table className="grid">
              <thead>
                <tr><th>{res.y_label || ""} ＼ {res.x_label}</th>{res.x_values.map((x) => <th key={x}>{x}</th>)}</tr>
              </thead>
              <tbody>
                {res.cells.map((row, yi) => (
                  <tr key={yi}>
                    <td><b>{res.y_values[yi] || m.label}</b></td>
                    {row.map((c, xi) => <td key={xi} style={heat(c[metric])}>{fmtMetric(m.unit, c[metric])}</td>)}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Section>
    </>
  );
}

const DEFAULT_METRICS = [
  { key: "pl02_coverage_pct", label: "PL-02 공급률", unit: "%" },
  { key: "pl02_short", label: "PL-02 부족(계약 기간)", unit: "EA" },
  { key: "pl01_schedule", label: "PL-01 제안(계약 기간)", unit: "EA" },
  { key: "penalty_backlog_krw", label: "전량 약속 시 지체상금(지연분)", unit: "원" },
  { key: "penalty_contract_krw", label: "계약 총액 기준 지체상금", unit: "원" },
  { key: "revenue_schedule_krw", label: "제안 기준 매출", unit: "원" },
  { key: "contrib_schedule_krw", label: "제안 기준 공헌이익", unit: "원" },
];

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
