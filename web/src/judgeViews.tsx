import { useState } from "react";
import type { Department, Flag, LaneAction, OptCompare, ProductEval, View } from "./types";
import { CompareView, Kpi, Mode, ScheduleView, Section, snapSource } from "./panels";
import { BASIS_LABEL, EVENT_LABEL, ea, eok, pct } from "./format";
import { canEvent } from "./perm";

// 판단·제안 페이지(이슈 #49): 지표 → PL-02 월별 공급(기본/비교 전환) → 플래그와 물을 곳, 옆에 내 할 일·숫자의 출처.
// 숫자는 모두 엔진 평가(evaluation)와 OPT-01 비교(compare_opt01)에서 온다. 화면은 다시 계산하지 않는다.

const LEVEL_ORDER = { 차단: 0, 위험: 1, 미확인: 2 } as const;

// 공급 출처별 막대 색(디자인 시스템 의미색): 기존 = 중립, OPT-02 = warn, OPT-01 = accent, 그 밖 = ok
const SEG_ORDER = ["base", "OPT-02", "MASIL-ADJ", "CUST-REL", "OPT-01"];
const SEG_CLS: Record<string, string> = { base: "seg-base", "OPT-02": "seg-opt02", "OPT-01": "seg-opt01" };
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

function SupplyChart({ v }: { v: View }) {
  const ev = v.state.evaluation;
  const pe = ev.products["PL-02"];
  const c = ev.compare_opt01;
  const [alt, setAlt] = useState(false);
  if (!pe) return null;
  const cols = columns(pe, c, alt && !!c);
  const H = 190;
  const max = Math.max(1, ...cols.map((col) => Math.max(col.demand, col.segs.reduce((a, s) => a + s.qty, 0))));
  const side = c ? (alt ? c.alt : c.base) : undefined;
  const used = [...new Set(cols.flatMap((col) => col.segs.map((s) => s.basis)))].sort((a, b) => SEG_ORDER.indexOf(a) - SEG_ORDER.indexOf(b));
  const allCond = (b: string) => cols.every((col) => col.segs.every((s) => s.basis !== b || s.conditional));
  const anyCond = cols.some((col) => col.segs.some((s) => s.conditional));
  return (
    <Section
      title="PL-02 월별 공급과 부족"
      mode={["real"]}
      note={`요구 월 ${ea(pe.monthly_demand)} EA(점선) · 계약 ${ev.scenario.contract_months}개월 · 막대는 공급 출처별로 쌓았다. 증설·전환 능력은 적격일 다음 달부터(R3). 막대 위 빨간 숫자 = 그 달 부족, 아래 = 제안 EA. 출처: ${snapSource(v)}.`}
    >
      {c && (
        <div className="row" role="group" aria-label="OPT-01 적격 기간 시나리오">
          <button className={alt ? "ghost small" : "small"} aria-pressed={!alt} onClick={() => setAlt(false)}>기본(적격 {c.base.validation_weeks}주, 제출 기준)</button>
          <button className={alt ? "small" : "ghost small"} aria-pressed={alt} onClick={() => setAlt(true)}>원본 "재검증 불필요"(적격 {c.alt.validation_weeks}주)</button>
          <span className={alt ? "small warn-text" : "muted small"}>{alt ? "대안 시나리오: 제출 기준이 아니다. 테두리 칸이 기본과 다르다." : "제출·판단은 이 시나리오로만 한다."}</span>
        </div>
      )}
      <div className="scroll">
        <div className="supchart" role="img" aria-label={`PL-02 월별 공급과 부족${alt ? " (비교 시나리오)" : ""}. 수치는 아래 월별 표에 있다.`}>
          {cols.map((col) => (
            <div key={col.month} className={`supcol ${col.changed ? "is-changed" : ""}`}>
              <span className="supshort">{col.short ? ea(col.short) : ""}</span>
              <div className="supbar" style={{ height: H }}>
                <div className="supdemand" style={{ bottom: (col.demand / max) * H }} title={`요구 ${ea(col.demand)} EA`} />
                {col.segs.map((s) => (
                  <div
                    key={s.basis}
                    className={`supseg ${SEG_CLS[s.basis] ?? "seg-other"} ${s.conditional ? "cond" : ""}`}
                    style={{ height: Math.max(1, (s.qty / max) * H) }}
                    title={`${BASIS_LABEL[s.basis] ?? s.basis} ${ea(s.qty)} EA${s.conditional ? " (조건부)" : ""}`}
                  />
                ))}
              </div>
              <span className="supmonth">{col.month.slice(2)}</span>
              <span className="supsched">{ea(col.schedule)}</span>
            </div>
          ))}
        </div>
      </div>
      <div className="legend">
        {used.map((b) => (
          <span key={b} className="row">
            <span aria-hidden="true" className={`swatch ${SEG_CLS[b] ?? "seg-other"} ${allCond(b) ? "cond" : ""}`} />
            {SEG_SHORT[b] ?? b}
            {allCond(b) ? "(조건부)" : ""}
          </span>
        ))}
        {anyCond && (
          <span className="row">
            <span aria-hidden="true" className="swatch seg-base cond" />
            옅은 점선 = 조건부(승인·선행 회신 전)
          </span>
        )}
        <span className="row">
          <span aria-hidden="true" className="swatch-line" />
          요구
        </span>
      </div>
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
    <Section title={`미확인·위험·차단 (${flags.length})`} mode={["real"]} note="무엇이 판단을 막는지와, 그것을 누구에게 물었는지(열린 확인 요청)를 함께 보인다.">
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
          <h3>직전 사건의 변화</h3>
          <p className="small">
            <b>{EVENT_LABEL[last.event.type] ?? last.event.type}</b> — {(last.changes ?? []).join(" · ")}
          </p>
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
        <span className="modes"><Mode kind="real" /></span>
      </header>
      {!dept ? (
        <p className="small">위 막대에서 부서를 고르면 이 판단으로 지금 할 일이 보인다.</p>
      ) : first ? (
        <p className="small">
          <b>{dept.label}</b>: {first.label}. {v.state.bid ? `입찰 ${v.state.bid.state} · v${v.state.bid.version}.` : ""}
        </p>
      ) : (
        <p className="small">
          <b>{dept.label}</b>: 지금 이 판단으로 넣을 사건은 없다. {actions.find((a) => !a.enabled)?.reason ?? ""}
        </p>
      )}
      <div className="row">
        {first && <button onClick={() => onAction(first)}>{first.label}</button>}
        <button className="ghost" disabled={!dept} title={dept ? "" : "부서를 먼저 고른다"} onClick={onTrySim}>
          {v.session.kind === "sim" ? "시뮬레이션 페이지로" : "시뮬레이션에서 먼저 보기"}
        </button>
      </div>
      {v.session.kind === "main" && <p className="muted small" style={{ marginBottom: 0 }}>시뮬레이션에서 먼저 보기 = 지금 상태를 분기한 시뮬레이션 세션을 만들고 그쪽으로 간다(기준 세션은 그대로).</p>}
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
    <Section title="숫자의 출처" mode={["real"]} note="원본 기록·계산·합성·가정·회신 반영을 구분한다. 전체 가정은 근거 › 가정 페이지.">
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
      <Section title="판단 요약" mode={["real"]} note={`숫자는 결정 모델(엔진)이 원본에서 다시 계산하고, 승인은 권한 부서가 한다. 입력은 고정 필드만 쓴다(LLM 현재 제외). 여유·공급 출처: ${snapSource(v)}. 생산 라인 보드의 배치 실적과는 규모가 다르다(여유·증설안 페이지).`}>
        <div className="kpis kpis-5">
          <Kpi label="PL-02 더마 제안 가능" value={`${ea(p2?.schedule_total)} EA`} sub={`요구 ${ea((p2?.monthly_demand ?? 0) * ev.scenario.contract_months)} 중 ${pct(p2?.coverage_pct)} · ${l2?.qty_status ?? "접수 전"}`} tone={p2 && p2.coverage_pct < 100 ? "warn" : "ok"} />
          <Kpi label="PL-01 기초" value={l1?.qty_status === "미확정" || !l1 ? "수량 미확정" : `${ea(p1?.schedule_total)} EA`} sub={`확인 전 상한 월 ${ea(p1?.months?.[0]?.supply)} EA · 요구 월 ${ea(p1?.monthly_demand)}`} tone="bad" />
          <Kpi label="전량 약속 시 지체상금(지연분)" value={eok((p1?.penalty_backlog_krw ?? 0) + (p2?.penalty_backlog_krw ?? 0))} sub={`계약 총액 기준 ${eok(ev.penalty_contract_krw)} (월 ${eok(ev.penalty_contract_monthly_krw)})`} tone="bad" />
          <Kpi label="연 공헌이익(요구 전량, 원료비 기준)" value={eok((p1?.contrib_request_krw ?? 0) + (p2?.contrib_request_krw ?? 0), 1)} sub={`제안 기준 ${eok((p1?.contrib_schedule_krw ?? 0) + (p2?.contrib_schedule_krw ?? 0), 1)} · 단가 인하 ${pct(ev.scenario.price_cut * 100)}`} tone="ok" />
          {c && <Kpi label="OPT-01 최초 반영 월" value={c.base.first_supply_month || "없음"} sub={`설치 ${c.base.install_date} + 적격 ${c.base.validation_weeks}주 · 원본 문구면 ${c.alt.first_supply_month || "없음"}`} tone={c.base.usable ? "ok" : "warn"} />}
        </div>
      </Section>
      <div className="split">
        <div className="split-main">
          <SupplyChart v={v} />
          <FlagsCard v={v} />
          <details className="more-tables">
            <summary>월별 표 전체 보기: 제안 일정(PL-02·PL-01)과 OPT-01 비교 표</summary>
            <div className="split-main">
              <ScheduleView v={v} />
              <CompareView v={v} />
            </div>
          </details>
        </div>
        <div className="split-side">
          <MyTurn v={v} dept={dept} onAction={onAction} onTrySim={onTrySim} />
          <SourcesCard v={v} />
        </div>
      </div>
    </>
  );
}
