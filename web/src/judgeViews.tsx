import { useMemo, useState } from "react";
import type { Department, Flag, LaneAction, OptCompare, ProductEval, View } from "./types";
import { Kpi, ScheduleView, Section } from "./panels";
import { Link } from "./router";
import { BASIS_LABEL, EVENT_LABEL, ea, eok, pct } from "./format";
import { canEvent } from "./perm";
import { BASE, EChart, INK, INK2, LINE, PAPER, RED, axis } from "./chart";

// 판단·제안 페이지(이슈 #49): 지표 → PL-02 월별 공급(기본/비교 전환) → 플래그와 물을 곳, 옆에 내 할 일·숫자의 출처.
// 숫자는 모두 엔진 평가(evaluation)와 OPT-01 비교(compare_opt01)에서 온다. 화면은 다시 계산하지 않는다.

const LEVEL_ORDER = { 차단: 0, 위험: 1, 미확인: 2 } as const;

// 공급 출처 쌓는 순서(아래부터)
const SEG_ORDER = ["base", "OPT-02", "MASIL-ADJ", "CUST-REL", "OPT-01"];
const SEG_SHORT: Record<string, string> = { base: "기존 여유", "OPT-02": "OPT-02", "OPT-01": "OPT-01", "MASIL-ADJ": "Masil 조정", "CUST-REL": "고객 반환" };

type Seg = { basis: string; qty: number; conditional: boolean };
type Col = { month: string; demand: number; segs: Seg[]; schedule: number; short: number; changed: boolean };

// 비교 모드는 OPT-01 공급만 바뀐다(compare.go). 나머지 출처는 기본 평가 그대로 쓴다.
function columns(pe: ProductEval, c: OptCompare | undefined, alt: boolean): Col[] {
  const cm = new Map((c?.months ?? []).map((m) => [m.month, m]));
  return pe.months.map((m) => {
    let segs: Seg[] = (m.supplies ?? []).map((s) => ({ ...s }));
    let schedule = m.schedule;
    let short = m.short;
    let changed = false;
    const x = cm.get(m.month);
    if (alt && c && x) {
      const cond = segs.find((s) => s.basis === "OPT-01")?.conditional ?? c.alt.conditional;
      segs = segs.filter((s) => s.basis !== "OPT-01");
      if (x.alt_opt01 > 0) segs.push({ basis: "OPT-01", qty: x.alt_opt01, conditional: cond });
      schedule = x.alt_schedule;
      short = x.alt_short;
      changed = x.alt_opt01 !== x.base_opt01;
    }
    segs.sort((a, b) => SEG_ORDER.indexOf(a.basis) - SEG_ORDER.indexOf(b.basis));
    return { month: m.month, demand: m.demand, segs, schedule, short, changed };
  });
}

// 출처별 채움·무늬(팔레트 다섯 색): 기존 여유 = 짙은 회색, OPT-02 = 검정, OPT-01 = 흰 바탕 빗금, Masil 조정 = 옅은 회색,
// 고객 반환 = 흰 바탕 점. 조건부(승인·선행 회신 전)는 흰 바탕 + 그 출처 색 파선 테두리(투명도는 팔레트 밖 회색을 만들어 쓰지 않는다).
// 부족 = 빨강 테두리(경고), 요구 = 검정 파선.
const DIAG = { symbol: "rect", symbolSize: 1, dashArrayX: [1, 0], dashArrayY: [2, 4], rotation: Math.PI / 4, color: INK };
const DOTS = { symbol: "circle", symbolSize: 0.8, dashArrayX: [[1, 5]], dashArrayY: [1, 5], color: INK };
const condStyle = (b: string) => {
  const st = SEG_STYLE[b] ?? {};
  const edge = (st.borderColor as string | undefined) ?? (st.color as string | undefined) ?? INK2;
  return { color: PAPER, borderColor: edge === PAPER ? INK : edge, borderType: "dashed", borderWidth: 1.5, ...(st.decal ? { decal: st.decal } : {}) };
};
const SEG_STYLE: Record<string, Record<string, unknown>> = {
  base: { color: INK2 },
  "OPT-02": { color: INK },
  "OPT-01": { color: PAPER, borderColor: INK, borderWidth: 1, decal: DIAG },
  "MASIL-ADJ": { color: LINE, borderColor: INK2, borderWidth: 1 },
  "CUST-REL": { color: PAPER, borderColor: INK2, borderWidth: 1, decal: DOTS },
};

function SupplyChart({ v }: { v: View }) {
  const ev = v.state.evaluation;
  const pe = ev.products["PL-02"];
  const c = ev.compare_opt01;
  const [alt, setAlt] = useState(false);
  const cols = useMemo(() => (pe ? columns(pe, c, alt && !!c) : []), [pe, c, alt]);
  const option = useMemo(() => {
    const used = [...new Set(cols.flatMap((col) => col.segs.map((s) => s.basis)))].sort((a, b) => SEG_ORDER.indexOf(a) - SEG_ORDER.indexOf(b));
    const months = cols.map((col) => col.month);
    const supply = used.map((b) => {
      const all = cols.every((col) => col.segs.every((s) => s.basis !== b || s.conditional));
      return {
        name: `${SEG_SHORT[b] ?? b}${all ? "(조건부)" : ""}`,
        type: "bar",
        stack: "supply",
        barMaxWidth: 34,
        itemStyle: all ? condStyle(b) : { ...(SEG_STYLE[b] ?? { color: LINE, borderColor: INK2, borderWidth: 1 }) },
        data: cols.map((col) => {
          const s = col.segs.find((x) => x.basis === b);
          if (!s || !s.qty) return null;
          return s.conditional && !all ? { value: s.qty, itemStyle: condStyle(b) } : { value: s.qty };
        }),
      };
    });
    const short = {
      name: "부족",
      type: "bar",
      stack: "supply",
      barMaxWidth: 34,
      itemStyle: { color: PAPER, borderColor: RED, borderWidth: 1.5 },
      data: cols.map((col) => (col.short ? col.short : null)),
    };
    const demand = {
      name: "요구",
      type: "line",
      step: "middle",
      symbol: "none",
      lineStyle: { color: INK, type: "dashed", width: 1.5 },
      itemStyle: { color: INK },
      data: cols.map((col) => col.demand),
      z: 5,
    };
    return {
      ...BASE,
      grid: { left: 52, right: 12, top: 40, bottom: 28 },
      tooltip: {
        ...(BASE.tooltip as object),
        trigger: "axis",
        axisPointer: { type: "shadow", shadowStyle: { color: LINE } },
        formatter: (ps: { dataIndex: number }[]) => {
          const col = cols[ps[0]?.dataIndex ?? 0];
          if (!col) return "";
          const rows = col.segs.map((s) => `${BASIS_LABEL[s.basis] ?? s.basis} <b>${ea(s.qty)}</b> EA${s.conditional ? " (조건부)" : ""}`);
          return [`<b>${col.month}${col.changed ? " · 기본과 다름" : ""}</b>`, ...rows, `제안 <b>${ea(col.schedule)}</b> EA`, `요구 ${ea(col.demand)} EA`, col.short ? `<span style="color:${RED}">부족 <b>${ea(col.short)}</b> EA</span>` : "부족 없음"].join("<br>");
        },
      },
      legend: { ...(BASE.legend as object), data: [...supply.map((x) => x.name), "부족", "요구"] },
      xAxis: axis({
        type: "category",
        data: months,
        splitLine: { show: false },
        axisLabel: { color: INK2, formatter: (m: string) => (cols.find((x) => x.month === m)?.changed ? `{b|${m.slice(2)}}` : m.slice(2)), rich: { b: { fontWeight: "bold", color: INK } } },
      }),
      yAxis: axis({ type: "value", axisLine: { show: false }, axisLabel: { color: INK2, formatter: (n: number) => (n >= 10000 ? `${Math.round(n / 1000)}k` : ea(n)) } }),
      series: [...supply, short, demand],
    };
  }, [cols]);
  if (!pe) return null;
  const side = c ? (alt ? c.alt : c.base) : undefined;
  const anyCond = cols.some((col) => col.segs.some((s) => s.conditional));
  return (
    <Section
      title="PL-02 월별 공급과 부족"
      mode={["real"]}
      note={`단위 EA/월 · 요구 월 ${ea(pe.monthly_demand)} EA(검정 파선) · 계약 ${ev.scenario.contract_months}개월. 공급을 출처별로 쌓고 맨 위에 부족(빨강 테두리)을 표시합니다. 증설·전환 능력은 적격일 다음 달부터 반영됩니다(R3).${anyCond ? " 흰 바탕에 파선 테두리는 조건부(승인·선행 회신 전) 공급입니다." : ""} 막대에 마우스를 올리면 해당 월 수치가 표시됩니다.`}
    >
      {c && (
        <div className="row" role="group" aria-label="OPT-01 적격 기간 시나리오">
          <button className={alt ? "ghost small" : "small"} aria-pressed={!alt} onClick={() => setAlt(false)}>기본(적격 {c.base.validation_weeks}주, 제출 기준)</button>
          <button className={alt ? "small" : "ghost small"} aria-pressed={alt} onClick={() => setAlt(true)}>원본 "재검증 불필요"(적격 {c.alt.validation_weeks}주)</button>
          <span className={alt ? "small warn-text" : "muted small"}>{alt ? "대안 시나리오이며 제출 기준이 아닙니다. 굵게 표시된 달이 기본과 다릅니다." : "제출·판단은 이 시나리오 기준으로만 진행합니다."}</span>
        </div>
      )}
      <EChart option={option} height={320} label={`PL-02 월별 공급과 부족${alt ? " (비교 시나리오)" : ""}. 수치는 아래 월별 표에서 확인할 수 있습니다.`} />
      {side && (
        <p className="small" style={{ marginBottom: 0 }}>
          {alt ? "비교" : "기본"}: 제안 <b>{ea(side.schedule_total)} EA</b>(공급률 {pct(side.coverage_pct)}) · 부족 {ea(side.short_total)} EA · OPT-01 최초 반영 {side.first_supply_month || "없음"} · 전량 약속 시 지연분 노출 {eok(side.penalty_backlog_krw)}
          {alt && c && <span className="warn-text"> · 기본 대비 제안 {side.schedule_total - c.base.schedule_total >= 0 ? "+" : ""}{ea(side.schedule_total - c.base.schedule_total)} EA</span>}
        </p>
      )}
    </Section>
  );
}

// 플래그마다 물을 곳: 그 규칙을 막고 있는 열린 확인 요청(blocks에 규칙 번호가 든 것)과 대상 부서.
function FlagsCard({ v }: { v: View }) {
  const s = v.state;
  const flags = [...(s.evaluation.flags ?? [])].sort((a, b) => LEVEL_ORDER[a.level] - LEVEL_ORDER[b.level]);
  const open = (s.requests ?? []).filter((r) => r.state !== "반영");
  const askFor = (f: Flag) => open.filter((r) => (r.blocks ?? []).includes(f.rule));
  const last = s.log?.[s.log.length - 1];
  return (
    <Section title={`미확인·위험·차단 (${flags.length})`} mode={["real"]} note="판단을 막는 항목과, 해당 항목을 확인 요청한 대상(열린 확인 요청)을 함께 표시합니다.">
      <ul className="flags">
        {flags.map((f) => {
          const asks = askFor(f);
          return (
            <li key={f.code} className={`flag flag-${f.level}`}>
              <span className="flag-level">{f.level}</span>
              <span className="rule">{f.rule}</span> {f.message}
              {asks.length > 0 && (
                <span className="asks">
                  {asks.map((r) => (
                    <span key={r.request_id} className={`ask ${r.state === "기한 초과" ? "is-late" : ""}`}>
                      물을 곳 {r.request_id} · {r.to_role}
                      {r.state === "기한 초과" ? " · 기한 초과" : ` · ~${r.due_date.slice(5)}`}
                    </span>
                  ))}
                </span>
              )}
            </li>
          );
        })}
      </ul>
      {last && (
        <>
          <h3>직전 사건의 변화 · {EVENT_LABEL[last.event.type] ?? last.event.type}</h3>
          <ul className="change-list small">
            {(last.changes ?? []).slice(0, 3).map((c) => <li key={c}>{c}</li>)}
          </ul>
          {(last.changes ?? []).length > 3 && (
            <details className="table-view">
              <summary className="small">변경 {(last.changes ?? []).length - 3}개 더 보기</summary>
              <ul className="change-list small">
                {(last.changes ?? []).slice(3).map((c) => <li key={c}>{c}</li>)}
              </ul>
            </details>
          )}
        </>
      )}
      {s.last_blocks && s.last_blocks.length > 0 && <p className="blocked">제출 차단: {s.last_blocks.join(" / ")}</p>}
    </Section>
  );
}

// 내 부서가 지금 할 일(내 업무와 같은 규칙: 상태로 가능한 사건 × 부서 권한, 대행 제외)
function MyTurn({ v, dept, onAction, onTrySim }: { v: View; dept?: Department; onAction: (a: LaneAction) => void; onTrySim: () => void }) {
  const actions = (v.lanes ?? [])
    .flatMap((l) => l.can ?? [])
    .filter((a) => {
      const req = (v.state.requests ?? []).find((r) => r.request_id === a.request_id);
      const c = canEvent(v, dept, a.event, req);
      return c.ok && !c.proxy;
    });
  const first = actions.find((a) => a.enabled);
  return (
    <section className={`card ${first ? "now-card has-action" : ""}`}>
      <header className="card-head">
        <h2>내가 할 일</h2>
      </header>
      {!dept ? (
        <p className="small">로그인한 계정의 역할을 권한표에서 찾지 못해 현재 할 일을 표시할 수 없습니다. 부서 계정으로 다시 로그인해 주세요.</p>
      ) : first ? (
        <p className="small">
          <b>{dept.label}</b>: {first.label}. {v.state.bid ? `입찰 ${v.state.bid.state} · v${v.state.bid.version}.` : ""}
        </p>
      ) : (
        <p className="small">
          <b>{dept.label}</b>: 현재 이 판단 단계에서 입력할 사건이 없습니다. {actions.find((a) => !a.enabled)?.reason ?? ""}
        </p>
      )}
      <div className="row">
        {first && <button onClick={() => onAction(first)}>{first.label}</button>}
        <button className="ghost" disabled={!dept} title={dept ? (v.session.kind === "main" ? "현재 상태를 분기한 시뮬레이션 세션을 생성하고 이동합니다. 기준 세션은 변경되지 않습니다." : "") : "로그인한 계정의 역할을 찾을 수 없습니다. 다시 로그인해 주세요."} onClick={onTrySim}>
          {v.session.kind === "sim" ? "시뮬레이션 페이지로" : "시뮬레이션에서 먼저 보기"}
        </button>
      </div>
    </section>
  );
}

// 판단에 쓴 핵심 숫자의 출처 구분(원본·계산·합성·가정·회신 반영)
function SourcesCard({ v }: { v: View }) {
  const ev = v.state.evaluation;
  const sc = ev.scenario;
  const l2 = ev.lines.find((l) => l.line_id === "L-02");
  const o1 = ev.options.find((o) => o.option_id === "OPT-01");
  const bid = v.state.bid;
  const rows: [string, string, string][] = [];
  if (l2) {
    rows.push([`L-02 기본 여유 ${ea(l2.spare_annual)} EA/연`, "계산", `능력 스냅샷 ${l2.snapshot_date}`]);
    rows.push([`PL-02 Masil 보호 ${ea(l2.masil_protected)} EA`, "합성", "Masil 발주 → L-02 매핑"]);
  }
  rows.push([`환율 ${ev.fx.toLocaleString("ko-KR")}`, "원본", `${ev.fx_date} 종가`]);
  rows.push([`납품 개시 ${sc.delivery_start}`, sc.start_confirmed ? "회신 반영" : "가정", sc.start_confirmed ? "바이어 회신" : "A-04"]);
  if (o1) rows.push([`OPT-01 적격 ${o1.validation_weeks}주`, "가정", "A-08"]);
  rows.push([`능력 단위 ${sc.capacity_period === "monthly" ? "월간" : "연간"}`, sc.unit_confirmed ? "회신 반영" : "가정", sc.unit_confirmed ? "생산관리 회신" : "D6, CR-01"]);
  if (bid) rows.push([`지체상금 월 ${pct(bid.penalty_rate_monthly * 100, 0)}`, "원본", "RFP"]);
  return (
    <Section title="숫자의 출처" mode={["real"]} note="원본 기록·계산·합성·가정·회신 반영을 구분해 표시합니다. 전체 가정은 근거 › 가정 페이지에서 확인할 수 있습니다.">
      <ul className="tasks">
        {rows.map(([what, kind, src]) => (
          <li key={what}>
            <div>{what}</div>
            <div>
              <span className={`tag tag-${kind.replace(/\s+/g, "-")}`}>{kind}</span>
              <span className="src">{src}</span>
            </div>
          </li>
        ))}
      </ul>
    </Section>
  );
}

export function JudgeView({ v, dept, onAction, onTrySim }: { v: View; dept?: Department; onAction: (a: LaneAction) => void; onTrySim: () => void }) {
  const s = v.state;
  const ev = s.evaluation;
  const p1 = ev.products["PL-01"];
  const p2 = ev.products["PL-02"];
  const l1 = s.lines?.find((l) => l.product_id === "PL-01");
  const l2 = s.lines?.find((l) => l.product_id === "PL-02");
  const c = ev.compare_opt01;
  return (
    <>
      <Section title="판단 요약" mode={["real"]} note={s.bid ? "숫자는 엔진이 원본에서 다시 계산하고, 승인은 권한 부서가 진행합니다. 출처는 '숫자의 출처'에서 확인할 수 있습니다." : "RFP 접수 전 사전 계산입니다. RFP 원본 값과 능력 스냅샷으로 미리 계산했으며, RFP를 접수하면(E1) 확인 요청과 함께 다시 계산합니다."}>
        {!s.bid && <span className="tag pre-calc">접수 전 사전 계산</span>}
        <div className="kpis kpis-5">
          <Kpi label="PL-02 제안 가능" value={`${ea(p2?.schedule_total)} EA`} sub={`요구 ${ea((p2?.monthly_demand ?? 0) * ev.scenario.contract_months)} 중 ${pct(p2?.coverage_pct)} · ${l2?.qty_status ?? "접수 전"}`} tone={p2 && p2.coverage_pct < 100 ? "warn" : "ok"} />
          <Kpi label="PL-01 기초" value={l1?.qty_status === "미확정" || !l1 ? "수량 미확정" : `${ea(p1?.schedule_total)} EA`} sub={`확인 전 상한 월 ${ea(p1?.months?.[0]?.supply)} EA · 요구 월 ${ea(p1?.monthly_demand)}`} tone="bad" />
          <Kpi label="지체상금(전량 약속 시)" value={eok((p1?.penalty_backlog_krw ?? 0) + (p2?.penalty_backlog_krw ?? 0))} sub={`계약 총액 기준 ${eok(ev.penalty_contract_krw)} (월 ${eok(ev.penalty_contract_monthly_krw)})`} tone="bad" />
          <Kpi label="연 공헌이익(요구 전량)" value={eok((p1?.contrib_request_krw ?? 0) + (p2?.contrib_request_krw ?? 0), 1)} sub={`제안 기준 ${eok((p1?.contrib_schedule_krw ?? 0) + (p2?.contrib_schedule_krw ?? 0), 1)} · 단가 인하 ${pct(ev.scenario.price_cut * 100)}`} tone="ok" />
          {c && <Kpi label="OPT-01 최초 반영" value={c.base.first_supply_month || "없음"} sub={`설치 ${c.base.install_date} + 적격 ${c.base.validation_weeks}주 · 원본 문구면 ${c.alt.first_supply_month || "없음"}`} tone={c.base.usable ? "ok" : "warn"} />}
        </div>
      </Section>
      <div className="split judge-split">
        <div className="split-main">
          <SupplyChart v={v} />
          <FlagsCard v={v} />
        </div>
        <div className="split-side">
          <MyTurn v={v} dept={dept} onAction={onAction} onTrySim={onTrySim} />
          <SourcesCard v={v} />
        </div>
      </div>
      <details className="more-tables">
        <summary>월별 표 전체 보기: 제안 일정(PL-02·PL-01)</summary>
        <ScheduleView v={v} />
      </details>
      {c && (
        <p className="small muted">
          OPT-01 적격 기간 비교(기본 {c.base.validation_weeks}주 대 원본 "재검증 불필요")는 <Link to="/bids/C-201/capacity">여유·증설안</Link>에서 확인할 수 있습니다.
        </p>
      )}
    </>
  );
}
