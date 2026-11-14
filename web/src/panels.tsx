import { useLayoutEffect, useRef, useState, type ReactNode } from "react";
import type { View, ProductEval, LineEval, OptionEval, LogEntry, Reservation, ChurnSignal, Task } from "./types";
import { BASIS_LABEL, EVENT_LABEL, PRODUCT_LABEL, ea, eok, pct, slug, won } from "./format";
import { EmptyState } from "./brand";

// 화면마다 실제 작동 / 모의 / 설계 단계를 표시한다(A04).
export function Mode({ kind }: { kind: "real" | "mock" | "design" }) {
  const text = { real: "실제 작동", mock: "모의", design: "설계 단계" }[kind];
  return <span className={`mode mode-${kind}`}>{text}</span>;
}

// 카드(이슈 #69 밀집도): '실제 작동'은 페이지 머리에 한 번 보이고, 카드에는 모의·설계 단계가 섞일 때만 작동 표시를 붙인다.
// 안내문은 한 줄로 줄이고 넘치면 "자세히"로 펼친다(Note).
export function Section({ title, mode, children, note }: { title: string; mode: ("real" | "mock" | "design")[]; children: ReactNode; note?: ReactNode }) {
  const special = mode.some((m) => m !== "real");
  const order = { real: 0, mock: 1, design: 2 } as const; // 배지 순서를 화면마다 같게
  return (
    <section className="card">
      <header className="card-head">
        <h2>{title}</h2>
        {special && <span className="modes">{[...mode].sort((x, y) => order[x] - order[y]).map((m) => <Mode key={m} kind={m} />)}</span>}
      </header>
      {note && <Note>{note}</Note>}
      {children}
    </section>
  );
}

// 안내문 한 줄 접기: 한 줄을 넘칠 때만 "자세히" 단추가 생긴다.
export function Note({ children }: { children: ReactNode }) {
  const ref = useRef<HTMLParagraphElement>(null);
  const [open, setOpen] = useState(false);
  const [over, setOver] = useState(false);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el || open) return;
    const check = () => setOver(el.scrollHeight > el.clientHeight + 1);
    check();
    const ro = new ResizeObserver(check);
    ro.observe(el);
    return () => ro.disconnect();
  }, [children, open]);
  return (
    <div className="note">
      <p ref={ref} className={`muted small ${open ? "" : "note-clamp"}`}>{children}</p>
      {(over || open) && (
        <button type="button" className="note-more" aria-expanded={open} onClick={() => setOpen(!open)}>
          {open ? "접기" : "자세히"}
        </button>
      )}
    </div>
  );
}

// 수치 출처 표시(이슈 #27 → #69): 열 제목 아래 보조 줄 대신 제목에 툴팁(점선 밑줄)으로 출처·단위·기간을 붙이고,
// 표 아래 TableSrc 한 줄에 같은 출처를 모은다(머리글이 여러 줄로 커지지 않게).
export function ColHead({ main, sub, className }: { main: ReactNode; sub: string; className?: string }) {
  return (
    <th className={className}>
      <span className="th-main" title={sub}>{main}</span>
      <span className="sr-only"> ({sub})</span>
    </th>
  );
}

export function TableSrc({ children }: { children: ReactNode }) {
  return <p className="table-src">출처 · {children}</p>;
}

export function snapSource(v: View): string {
  const b = v.data_basis;
  return `능력 스냅샷 ${b.snapshot_file} ${b.snapshot_date}${b.future_snapshot ? "(미래 자료)" : ""} · capacity_qty = ${b.capacity_unit}`;
}

export function batchSource(v: View): string {
  const b = v.data_basis;
  return `배치 실적 ${b.batch_file} · ${b.t12_from}~${b.t12_to} 월평균`;
}

// 원본 간 규모 차이: 배치 실적과 능력 스냅샷은 연간·월간 어느 해석으로도 함께 맞지 않는다(표시만, 계산은 그대로).
export function SourceGapView({ v }: { v: View }) {
  const b = v.data_basis;
  // 밀집도 2단계(이슈 #71): 한 줄 경고 + "자세히"로 펼치는 설명·표
  return (
    <Section title="원본 간 규모 차이" mode={["real"]}>
      <details className="gap-fold">
        <summary>
          <span className="tag tag-가정">원본 간 불일치</span>
          <span className="gap-line">능력 스냅샷과 배치 실적의 규모가 맞지 않습니다. 판단은 스냅샷 기준(D6)으로 합니다.</span>
          <span className="gap-more">자세히</span>
        </summary>
        <p className="muted small">
          엔진의 여유·C-201 가용·제안 일정은 <b>{snapSource(v)}</b>를 사용합니다(D6). 생산 라인 보드의 월 생산·부하·상태는 <b>{batchSource(v)}</b>와 진행 주문 기록({b.order_file}, 기록 ~{b.cutoff})을 사용합니다. 두 원본은 capacity_qty를 연간·월간 어느 쪽으로 읽어도 실적과 스냅샷 가동률이 함께 맞지 않습니다. A05 알려진 한계 목록에 없는 불일치이므로, 판단은 스냅샷 기준으로 하고 배치 실적은 라인 보드의 비율(부하 ÷ 실적)에만 사용합니다.
        </p>
        <div className="scroll">
          <table className="grid">
            <thead>
              <tr>
                <th>라인</th><th>제품군</th>
                <ColHead main="월 생산" sub={`배치 실적 · EA/월 · ${b.t12_from}~${b.t12_to} 평균`} />
                <ColHead main="capacity_qty" sub={`능력 스냅샷 ${b.snapshot_date} · 단위 미기재`} />
                <ColHead main="스냅샷 가동률" sub="능력 스냅샷" />
                <ColHead main="월 능력(연간)" sub="연간으로 읽으면: capacity ÷ 12 · EA/월" />
                <ColHead main="실적 ÷ 월 능력" sub="연간 해석(엔진 기본, D6)" />
                <ColHead main="실적 ÷ capacity(월간)" sub="월간으로 읽으면: 실적 ÷ capacity · 월간 해석(CR-01 대안)" />
              </tr>
            </thead>
            <tbody>
              {(b.gaps ?? []).map((g) => (
                <tr key={g.line_id}>
                  <td>{g.line_id} <span className="muted small">{g.plant_id}</span></td>
                  <td>{PRODUCT_LABEL[g.product_id] ?? g.product_id}</td>
                  <td>{ea(g.t12_monthly)}</td>
                  <td>{ea(g.capacity_qty)}</td>
                  <td>{pct(g.utilization_pct, 0)}</td>
                  <td>{ea(Math.floor(g.annual_monthly_cap))}</td>
                  <td className={g.ratio_annual_pct > 100 ? "bad" : ""}>{pct(g.ratio_annual_pct, 0)}</td>
                  <td className={Math.abs(g.ratio_monthly_pct - g.utilization_pct) > 10 ? "bad" : ""}>{pct(g.ratio_monthly_pct, 1)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <TableSrc>월 생산 = {batchSource(v)} · capacity_qty·가동률 = 능력 스냅샷 {b.snapshot_date}(단위 미기재) · 연간 해석 = 엔진 기본(D6), 월간 해석 = CR-01 대안</TableSrc>
        <p className="muted small">빨간 칸은 연간 해석에서 실적이 능력을 넘거나(100% 초과), 월간 해석에서 실적 비율이 스냅샷 가동률과 10%p 넘게 차이 나는 경우입니다. 단위는 생산관리 확인 요청(CR-01)으로 확인합니다.</p>
      </details>
    </Section>
  );
}

export function Kpi({ label, value, sub, tone }: { label: string; value: string; sub: string; tone: "ok" | "warn" | "bad" }) {
  return (
    <div className={`kpi kpi-${tone}`}>
      <div className="kpi-label">{label}</div>
      <div className="kpi-value">{value}</div>
      <div className="kpi-sub">{sub}</div>
    </div>
  );
}

// 제안 품목과 라인×월 공급·부족(R7·R8, 필수 확장: 적격 능력일 분할 약속)
export function ScheduleView({ v }: { v: View }) {
  const s = v.state;
  return (
    <Section title="제안 일정: 라인×월 공급과 부족" mode={["real"]} note={`납품 개시 ${s.scenario.delivery_start}${s.scenario.start_confirmed ? "(바이어 확인)" : "(가정 A-04)"} · 계약 ${s.scenario.contract_months}개월. 증설·전환 능력은 적격일 다음 달부터 산입합니다(R3). 점선은 조건부 공급이며, 단위는 EA/월입니다. '기존' 공급은 능력 스냅샷 여유(Masil 보호 뒤) ÷ 12, OPT 공급은 증설 계획 원본(strat_capex_plan_cv)의 추가 능력(연간) ÷ 12입니다.`}>
      {["PL-02", "PL-01"].map((p) => {
        const pe = s.evaluation.products[p];
        const line = s.lines?.find((l) => l.product_id === p);
        if (!pe) return null;
        return <ProductTable key={p} pe={pe} status={line?.qty_status} conds={line?.conditions ?? []} />;
      })}
    </Section>
  );
}

function ProductTable({ pe, status, conds }: { pe: ProductEval; status?: string; conds: { code: string; text: string }[] }) {
  return (
    <div className="product">
      <div className="product-head">
        <b>{PRODUCT_LABEL[pe.product_id]}</b>
        <span className="muted small">
          {pe.line_id} · 요구 연 {ea(pe.annual_qty)} EA @USD {pe.unit_price_usd} → {won(pe.unit_krw)}/EA · 원료비 {won(pe.material_per_ea)} · 공헌이익 {won(pe.contrib_per_ea)}
        </span>
        {status && <span className={`pill pill-${slug(status)}`}>{status}</span>}
      </div>
      <div className="scroll">
        <table className="grid sticky-first">
          <thead>
            <tr>
              <th>월</th>
              {pe.months.map((m) => <th key={m.month}>{m.month.slice(2)}</th>)}
              <th>합계</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td>요구</td>
              {pe.months.map((m) => <td key={m.month}>{ea(m.demand)}</td>)}
              <td>{ea(pe.monthly_demand * pe.months.length)}</td>
            </tr>
            <tr>
              <td>공급(출처)</td>
              {pe.months.map((m) => (
                <td key={m.month}>
                  {(m.supplies ?? []).map((sp) => (
                    <div key={sp.basis} className={sp.conditional ? "sup sup-cond" : "sup"} title={BASIS_LABEL[sp.basis] ?? sp.basis}>
                      {sp.basis === "base" ? "기존" : sp.basis} {ea(sp.qty)}
                    </div>
                  ))}
                  {(m.supplies ?? []).length === 0 && <span className="muted">0</span>}
                </td>
              ))}
              <td />
            </tr>
            <tr className="row-strong">
              <td>제안</td>
              {pe.months.map((m) => <td key={m.month}>{ea(m.schedule)}</td>)}
              <td>{ea(pe.schedule_total)}</td>
            </tr>
            <tr className="row-bad">
              <td>부족</td>
              {pe.months.map((m) => <td key={m.month}>{m.short ? ea(m.short) : "-"}</td>)}
              <td>{ea(pe.short_total)}</td>
            </tr>
            <tr className="muted">
              <td>전량 약속 시 밀린 물량</td>
              {pe.months.map((m) => <td key={m.month}>{m.backlog ? ea(m.backlog) : "-"}</td>)}
              <td>{eok(pe.penalty_backlog_krw)}</td>
            </tr>
          </tbody>
        </table>
      </div>
      {conds.length > 0 && (
        <ul className="conds">
          {conds.map((c) => (
            <li key={c.code}>
              <code>{c.code}</code> {c.text}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

// OPT-01 비교 시나리오(이슈 #29): 기본(제출 기준) 대 원본 "재검증 불필요" 적용. 판단·제출은 기본으로만 한다.
export function CompareView({ v }: { v: View }) {
  const c = v.state.evaluation.compare_opt01;
  if (!c) return null;
  const { base: b, alt: a } = c;
  const months = c.months ?? [];
  const sign = (n: number) => (n > 0 ? "+" : "");
  const diff = (x: number, y: number) => (y - x === 0 ? "-" : `${sign(y - x)}${ea(y - x)}`);
  const monthNo = (m: string) => Number(m.slice(0, 4)) * 12 + Number(m.slice(5, 7));
  const monthDiff = (x: string, y: string) => (!x || !y ? (x === y ? "-" : "다름") : monthNo(y) - monthNo(x) === 0 ? "-" : `${sign(monthNo(y) - monthNo(x))}${monthNo(y) - monthNo(x)}개월`);
  const same = (x: string, y: string) => (x === y ? "-" : "다름");
  const status = (s: typeof b) => `${s.status}${s.conditional && s.usable ? " (조건부)" : ""}`;
  // 요약은 지표를 행으로, 두 시나리오를 열로 둔다(밀집도 2단계, 이슈 #71). 차이 = 비교 − 기본.
  const rows: { k: string; b: string; a: string; d: string }[] = [
    { k: "OPT-01 결정", b: status(b), a: status(a), d: same(status(b), status(a)) },
    { k: "설치 완료", b: b.install_date, a: a.install_date, d: same(b.install_date, a.install_date) },
    { k: "적격(재검증)", b: `${b.qualified_date} (${b.validation_weeks}주)`, a: `${a.qualified_date} (${a.validation_weeks}주)`, d: a.validation_weeks === b.validation_weeks ? "-" : `${sign(a.validation_weeks - b.validation_weeks)}${a.validation_weeks - b.validation_weeks}주` },
    { k: "산입 시작", b: b.usable ? `${b.start_month}~` : "반영 안 함", a: a.usable ? `${a.start_month}~` : "반영 안 함", d: b.usable && a.usable ? monthDiff(b.start_month, a.start_month) : same(String(b.usable), String(a.usable)) },
    { k: "최초 반영 월(계약 안)", b: b.first_supply_month || "없음", a: a.first_supply_month || "없음", d: monthDiff(b.first_supply_month, a.first_supply_month) },
    { k: "OPT-01 공급 합", b: ea(b.opt_qty_total), a: ea(a.opt_qty_total), d: diff(b.opt_qty_total, a.opt_qty_total) },
    { k: "PL-02 제안 합", b: ea(b.schedule_total), a: ea(a.schedule_total), d: diff(b.schedule_total, a.schedule_total) },
    { k: "부족 합", b: ea(b.short_total), a: ea(a.short_total), d: diff(b.short_total, a.short_total) },
    { k: "공급률", b: pct(b.coverage_pct), a: pct(a.coverage_pct), d: a.coverage_pct === b.coverage_pct ? "-" : `${sign(a.coverage_pct - b.coverage_pct)}${(a.coverage_pct - b.coverage_pct).toFixed(1)}%p` },
    { k: "지연분 노출", b: eok(b.penalty_backlog_krw), a: eok(a.penalty_backlog_krw), d: a.penalty_backlog_krw === b.penalty_backlog_krw ? "-" : `${sign(a.penalty_backlog_krw - b.penalty_backlog_krw)}${eok(a.penalty_backlog_krw - b.penalty_backlog_krw)}` },
    { k: "계약 총액 기준", b: eok(b.penalty_contract_krw), a: eok(a.penalty_contract_krw), d: a.penalty_contract_krw === b.penalty_contract_krw ? "-" : `${sign(a.penalty_contract_krw - b.penalty_contract_krw)}${eok(a.penalty_contract_krw - b.penalty_contract_krw)}` },
  ];
  const head = (label: string) => label.split(":")[0];
  return (
    <Section
      title="OPT-01 비교 시나리오: 적격 기간 가정 대 원본 '재검증 불필요'"
      mode={["real"]}
      note={
        <>
          <span className="tag tag-가정">대안 시나리오 · 제출 기준 아님</span> 기본은 OPT-01에도 설치 뒤 적격 기간(A-08)을 더합니다. 비교는 원본 {c.source}을 적용해 적격 기간 0주로 계산합니다. 결정·회신·납품 개시월·OPT-02는 기본과 같습니다. PL-02 수량이며, 단위는 EA/월입니다.
        </>
      }
    >
      <div className="scroll">
        <table className="grid cmp-table">
          <thead>
            <tr>
              <th>항목</th>
              <ColHead main={head(b.label)} sub={b.label} />
              <ColHead main={head(a.label)} sub={a.label} />
              <th>차이(비교 − 기본)</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.k}>
                <td>{r.k}</td>
                <td className="cmp-base">{r.b}</td>
                <td className={r.d !== "-" ? "warn" : ""}>{r.a}</td>
                <td className={r.d === "-" ? "muted" : ""}>{r.d}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="scroll">
        <table className="grid sticky-first">
          <thead>
            <tr>
              <th>PL-02 월</th>
              {months.map((m) => <th key={m.month}>{m.month.slice(2)}</th>)}
            </tr>
          </thead>
          <tbody>
            <tr><td>요구</td>{months.map((m) => <td key={m.month}>{ea(m.demand)}</td>)}</tr>
            <tr><td>OPT-01 공급 · 기본</td>{months.map((m) => <td key={m.month}>{m.base_opt01 ? ea(m.base_opt01) : "-"}</td>)}</tr>
            <tr><td>OPT-01 공급 · 비교</td>{months.map((m) => <td key={m.month} className={m.alt_opt01 !== m.base_opt01 ? "warn" : ""}>{m.alt_opt01 ? ea(m.alt_opt01) : "-"}</td>)}</tr>
            <tr className="row-strong">
              <td title="비교 − 기본">제안 차이</td>
              {months.map((m) => (
                <td key={m.month} className={m.alt_schedule !== m.base_schedule ? "warn" : "muted"}
                  title={`제안 기본 ${ea(m.base_schedule)} / 비교 ${ea(m.alt_schedule)} · 전체 공급 기본 ${ea(m.base_supply)} / 비교 ${ea(m.alt_supply)}`}>
                  {diff(m.base_schedule, m.alt_schedule)}
                </td>
              ))}
            </tr>
            <tr className="row-bad">
              <td title="두 값이 같으면 하나만 표시합니다">부족 · 기본 / 비교</td>
              {months.map((m) => (
                <td key={m.month} title={`기본 ${ea(m.base_short)} / 비교 ${ea(m.alt_short)}`}>
                  {!m.base_short && !m.alt_short ? "-" : m.base_short === m.alt_short ? ea(m.base_short) : `${ea(m.base_short)} / ${ea(m.alt_short)}`}
                </td>
              ))}
            </tr>
          </tbody>
        </table>
      </div>
      <TableSrc>계산 · PL-02 · EA/월 · 제안·전체 공급(기본/비교) 수량은 차이 칸의 툴팁에 표시됩니다</TableSrc>
    </Section>
  );
}

// 라인 여유(R1)와 증설·전환안 적격 능력일(R3)
// 밀집도 2단계(이슈 #71): 답(C-201 가용·월)을 라인 바로 옆에 두고, C-201 대상이 아닌 라인은 접는다.
// 신규 수주 두 열이 모두 0이면 열을 빼고 한 줄로 알린다. 증설·전환안은 설치·적격·산입을 '일정' 한 열로 모은다.
const isC201 = (l: LineEval) => l.product_id === "PL-01" || l.product_id === "PL-02";
const md = (d: string) => (d ? d.slice(5) : "-");

export function CapacityView({ v }: { v: View }) {
  const ev = v.state.evaluation;
  const targets = ev.lines.filter(isC201);
  const others = ev.lines.filter((l) => !isC201(l));
  const showOrders = targets.some((l) => l.new_orders !== 0 || l.hist_orders !== 0);
  const histSub = `원본 시계 · ${v.state.scenario.hist_basis === "gross" ? "총량(모의)" : "순증분"} · EA`;
  return (
    <Section title="라인 여유와 증설·전환안" mode={["real"]} note={`이 표는 엔진 판단 기준입니다. 출처: ${snapSource(v)}(단위 ${v.state.scenario.unit_confirmed ? "확인됨" : "미확인"}). 여유 = 능력 × (1 − 가동률). F1 가동률 가중 ${pct(ev.f1_weighted_util, 2)} / 단순 ${pct(ev.f1_simple_util, 2)}. 생산 라인 보드의 월 생산(배치 실적)과는 규모가 다릅니다. 아래 '원본 간 규모 차이'를 확인해 주세요.`}>
      <div className="scroll">
        <table className="grid">
          <thead>
            <tr>
              <th>라인</th><th className="c-left">공장</th><th className="c-left">제품군</th>
              <ColHead main="C-201 가용" sub="계산 · EA/년" />
              <ColHead main="월" sub="÷ 12 · EA/월" />
              <ColHead main="능력" sub={`스냅샷 ${v.data_basis.snapshot_date} · EA/년(D6)`} />
              <ColHead main="가동률" sub="스냅샷" />
              <ColHead main="기본 여유" sub="계산 · EA/년" />
              <ColHead main="Masil 보호" sub={`발주 ~${v.data_basis.cutoff} · EA`} />
              {showOrders && <ColHead main="신규 수주" sub="E5 · EA/년" />}
              {showOrders && <ColHead main="기준일 뒤 수주" sub={`기준일 뒤 신규 수주 · ${histSub}`} />}
            </tr>
          </thead>
          <tbody>
            {targets.map((l) => (
              <tr key={l.line_id}>
                <td>{l.line_id}</td><td className="c-left">{l.plant_id}</td><td className="c-left">{l.product_id}{l.dedicated && <span className="cell-sub">F1 전용</span>}</td>
                <td><b>{ea(l.base_avail_annual)}</b></td><td>{ea(l.base_monthly)}</td>
                <td>{ea(l.capacity_qty)}</td><td>{pct(l.utilization_pct, 0)}</td><td>{ea(l.spare_annual)}</td>
                <td>{ea(l.masil_protected)}{l.masil_deficit > 0 && <span className="cell-sub bad">부족 {ea(l.masil_deficit)}</span>}</td>
                {showOrders && <td>{ea(l.new_orders)}</td>}
                {showOrders && <td className={l.hist_orders ? "warn" : ""}>{ea(l.hist_orders)}</td>}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {!showOrders && <p className="muted small cap-line">신규 수주 없음(E5·원본 시계)</p>}
      <TableSrc>C-201 가용·월·기본 여유 = 계산 · 능력·가동률 = 능력 스냅샷 {v.data_basis.snapshot_date}(EA/년, D6) · Masil 보호 = 발주 ~{v.data_basis.cutoff}{showOrders ? " · 신규 수주 = E5 · 기준일 뒤 수주 = 원본 시계" : ""}</TableSrc>
      {others.length > 0 && (
        <details className="dfold">
          <summary>다른 라인 {others.length}개 · C-201 대상 아님</summary>
          <div className="scroll">
            <table className="grid dfold-table">
              <thead>
                <tr>
                  <th>라인</th><th className="c-left">공장</th><th className="c-left">제품군</th>
                  <ColHead main="능력" sub={`스냅샷 ${v.data_basis.snapshot_date} · EA/년(D6)`} />
                  <ColHead main="가동률" sub="스냅샷" />
                  <ColHead main="기본 여유" sub="계산 · EA/년" />
                </tr>
              </thead>
              <tbody>
                {others.map((l) => (
                  <tr key={l.line_id}>
                    <td>{l.line_id}</td><td className="c-left">{l.plant_id}</td><td className="c-left">{l.product_id}{l.dedicated ? " (F1 전용)" : ""}</td>
                    <td>{ea(l.capacity_qty)}</td><td>{pct(l.utilization_pct, 0)}</td><td>{ea(l.spare_annual)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </details>
      )}
      <div className="scroll">
        <table className="grid opt-table">
          <thead>
            <tr>
              <th>안</th><th className="c-left">대상</th>
              <th>투자</th><th>추가(연)</th><th className="c-left">결정</th><th>결정일</th>
              <ColHead main="일정" sub="설치 → 적격(재검증) → 산입 시작월(R3)" />
              <th className="c-left">선행조건</th><th className="c-left">단계</th><th>회수</th>
            </tr>
          </thead>
          <tbody>
            {ev.options.map((o: OptionEval) => {
              // 단계가 '결정(…)' 꼴이면 결정 열과 겹치는 앞부분을 빼고 괄호 안만 보인다(전체는 title).
              const phase = o.phase.startsWith(`${o.status}(`) && o.phase.endsWith(")") ? o.phase.slice(o.status.length + 1, -1) : o.phase;
              return (
                <tr key={o.option_id} className={o.usable ? "" : "muted"}>
                  <td>{o.option_id}<span className="cell-sub">{o.type}</span></td>
                  <td className="c-left" title={`${o.target_line_text} → ${o.line_id}(합성 매핑, A-10)`}>{o.line_id}</td>
                  <td>{eok(o.capex_krw, 0)}</td><td>{ea(o.added_annual_qty)}</td><td className="c-left">{o.status}</td>
                  <td>{o.decision_date}{o.decision_date_assumed && <span className="cell-sub" title="OPT 승인일 가정(A-09)">가정(A-09)</span>}</td>
                  <td title={`설치 ${o.install_date} → 적격 ${o.qualified_date}(재검증 ${o.validation_weeks}주) → 산입 ${o.start_month}~`}>
                    {md(o.install_date)} → {md(o.qualified_date)} → {o.start_month}~
                  </td>
                  <td className="c-left">{(o.pending_requests ?? []).length ? (o.pending_requests ?? []).map((r) => <span key={r} className="cell-line">{r}</span>) : "-"}</td>
                  <td className="c-left opt-phase" title={o.phase}>{phase}</td>
                  <td>{o.payback_months.toFixed(1)}개월</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      <TableSrc>일정 = 설치 → 적격(재검증 {ev.options.map((o) => `${o.option_id} ${o.validation_weeks}주`).join(" · ")}, A-08) → 산입 시작월(R3) · 대상 = 증설 계획 원본의 target_line <span className="tag tag-합성">합성 매핑</span>(A-10)</TableSrc>
    </Section>
  );
}

// 약속 원장: 기존 B2B(가동률 포함, A-06), Masil 보호(A-07), C-201 가예약(R2)
export function LedgerView({ v }: { v: View }) {
  const c = v.state.evaluation.commitments;
  const res = v.state.reservations ?? [];
  return (
    <Section title="약속 원장" mode={["real"]} note="기존 확정 약속은 C-201 제안으로 밀어낼 수 없습니다(R2). Masil 발주는 Corevia 수주장부 밖에 있어 합성 매핑으로 L-01·L-02에 연결합니다.">
      <div className="cols">
        <div>
          <h3>진행 중 B2B 주문 (원본, 가동률 포함 가정 A-06)</h3>
          <div className="scroll">
          <table className="grid b2b-table">
            <thead><tr><th>제품군</th><th>건수</th><th>수량</th><ColHead main="7월 추정" sub="7월 추정 납품 · 주문일 + 조건부 리드타임 중앙값(A-14)" /><ColHead main="8월 추정" sub="8월 추정 납품 · 주문일 + 조건부 리드타임 중앙값(A-14)" /><ColHead main="월 생산" sub={batchSource(v)} /></tr></thead>
            <tbody>
              {["PL-01", "PL-02"].map((p) => (
                <tr key={p}>
                  <td>{p}</td><td>{ea(c.count_by_product[p])}</td><td>{ea(c.by_product[p])}</td>
                  <td>{ea(c.est_due[p]?.["2026-07"])}</td><td>{ea(c.est_due[p]?.["2026-08"])}</td><td>{ea(c.t12_monthly_prod[p])}</td>
                </tr>
              ))}
            </tbody>
          </table>
          </div>
          <TableSrc>월 생산 = {batchSource(v)}</TableSrc>
          <p className="muted small">전체 {ea(c.in_progress_count)}건 · {ea(c.in_progress_qty)} EA. 추정 납기는 주문일 + 조건부 리드타임 중앙값입니다(A-14, 검산용). 대리점 주문 {ea(v.data_basis.agency_orders)}건 · {ea(v.data_basis.agency_qty)} EA는 포함되지 않습니다(A-17).</p>
        </div>
        <div>
          <h3>Masil 발주 (보호, Win 등급) <span className="tag">합성 매핑</span></h3>
          <div className="scroll">
          <table className="grid">
            <thead><tr><th>발주</th><th>SKU</th><th>제품군</th><th>수량</th><th>입고 필요</th></tr></thead>
            <tbody>
              {c.masil_spos.filter((s) => s.product_id === "PL-01" || s.product_id === "PL-02").map((s) => (
                <tr key={s.po_id}><td>{s.po_id}</td><td>{s.sku_id}</td><td>{s.product_id}</td><td>{ea(s.qty)}</td><td>{s.arrive_by}</td></tr>
              ))}
            </tbody>
          </table>
          </div>
        </div>
      </div>
      <h3>C-201 가예약 ({res.length}건)</h3>
      {res.length === 0 ? (
        <p className="muted small">현재 가예약이 없습니다. 제안을 제출하면(E7) 여기에 표시됩니다.</p>
      ) : (
        <ReservationTable res={res} />
      )}
    </Section>
  );
}

// 가예약(R2): 제품군 행 × 월 열. 칸 = 수량, 아래 작은 글씨 = 공급 출처(전체 이름·상태는 title). 밀집도 2단계(이슈 #71)
const basisShort = (b: string) => b.split("+").map((x) => (x === "base" ? "기존" : x)).join("+");
const basisLong = (b: string) => b.split("+").map((x) => BASIS_LABEL[x] ?? x).join(" + ");

function ReservationTable({ res }: { res: Reservation[] }) {
  const months = [...new Set(res.map((r) => r.month))].sort();
  const prods = [...new Set(res.map((r) => r.product_id))].sort();
  const at = new Map(res.map((r) => [`${r.product_id}|${r.month}`, r]));
  const bases = [...new Set(res.flatMap((r) => r.basis.split("+")))];
  return (
    <>
      <div className="scroll">
        <table className="grid sticky-first res-table">
          <thead>
            <tr>
              <th>제품군</th>
              {months.map((m) => <th key={m}>{m.slice(2)}</th>)}
              <th>합계</th>
            </tr>
          </thead>
          <tbody>
            {prods.map((p) => (
              <tr key={p}>
                <td>{PRODUCT_LABEL[p] ?? p}</td>
                {months.map((m) => {
                  const r = at.get(`${p}|${m}`);
                  return r ? (
                    <td key={m} title={`${p} ${m} · ${ea(r.qty)} EA · ${basisLong(r.basis)} · ${r.state}`}>
                      {ea(r.qty)}
                      <span className="res-basis">{basisShort(r.basis)}</span>
                    </td>
                  ) : (
                    <td key={m} className="muted">-</td>
                  );
                })}
                <td>{ea(res.filter((r) => r.product_id === p).reduce((s, r) => s + r.qty, 0))}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <TableSrc>계산 · EA/월 · 칸 아래 = 공급 출처({bases.map((x) => `${basisShort(x)} = ${BASIS_LABEL[x] ?? x}`).join(", ")})</TableSrc>
    </>
  );
}

// 기존 고객 이탈 신호(R12)와 반환 금지(R13): 수주(order_date)와 출하(ship_date)를 나눠 본다(W05 단초).
export function ChurnView({ v }: { v: View }) {
  const sigs = v.state.evaluation.churn ?? [];
  const sc = v.state.scenario;
  return (
    <Section
      title="기존 고객 이탈 신호 (R12) · 능력 반환 금지 (R13)"
      mode={["real"]}
      note={
        <>
          매출 비중 10% 이상 고객의 월 수주(order_date)와 출하(ship_date)를 금액 기준으로 나눠 확인합니다({v.data_basis.order_file}, 마감된 달 ~{sigs[0]?.month ?? "-"}).
          재무 메모(sales_memo_cv)는 출하 기준으로 "정상"이라 보고했고, 영업 회의록은 신규 발주 감소를 "단정 금지"로 기록했습니다 <span className="tag">부서 주장</span>.
          임계값은 설계값입니다(A-23). AT_RISK 고객의 진행 주문은 '검토'로 표시하고 보호를 유지하며, 영업 임원 확인(CR-09) 전에는 C-201에 능력을 반환하지 않습니다(A-24: {sc.churn_answered ? (sc.churn_revised ? "수정 확인, 반환 반영" : "수정 아님, 반환 0") : "회신 전, 반환 0"}).
        </>
      }
    >
      {sigs.map((s) =>
        s.status === "AT_RISK" || s.status === "WATCH" ? (
          <div key={s.customer_id} className="product">
            <div className="product-head"><ChurnHead s={s} /></div>
            <ChurnTable s={s} />
          </div>
        ) : (
          // 정상 고객은 머리 줄만 보이고 월별 표는 접는다(밀집도 2단계, 이슈 #71)
          <details key={s.customer_id} className="product churn-fold">
            <summary><ChurnHead s={s} /></summary>
            <ChurnTable s={s} />
          </details>
        ),
      )}
      {sigs.length > 0 && <TableSrc>단위 억 · 수주 = order_date, 출하 = ship_date · WATCH = 단월 수주/출하 0.85 미만 2개월 · AT_RISK = 3개월 수주/출하 0.8 미만 또는 YoY −15% 미만(A-23)</TableSrc>}
      {sigs.length === 0 && <p className="muted small">현재 매출 비중 10% 이상인 대상 고객이 없습니다.</p>}
    </Section>
  );
}

function ChurnHead({ s }: { s: ChurnSignal }) {
  return (
    <>
      <b>{s.customer_id}</b>{" "}
      <span className="muted small">매출 비중 {s.revenue_share_pct}% · 진행 주문 {ea(s.open_orders)}건(PL-01 {ea(s.open_qty?.["PL-01"])} · PL-02 {ea(s.open_qty?.["PL-02"])} EA){s.status === "AT_RISK" ? " → 검토(R13), 보호 유지" : ""}</span>{" "}
      <span className={`pill churn-${s.status}`}>{s.status}{s.since ? ` (${s.since}~)` : ""}</span>
    </>
  );
}

// 월 열은 같은 좁은 폭으로 고정하고 표는 내용 폭만 쓴다(.churn-table)
function ChurnTable({ s }: { s: ChurnSignal }) {
  const hs = s.history ?? [];
  const ratio = (x: number) => (x ? x.toFixed(3) : "-");
  return (
    <div className="scroll">
      <table className="grid sticky-first churn-table">
        <thead>
          <tr><th>월</th>{hs.map((h) => <th key={h.month}>{h.month.slice(2)}</th>)}</tr>
        </thead>
        <tbody>
          <tr><td title="order_date · 억">수주</td>{hs.map((h) => <td key={h.month}>{eok(h.booking_krw, 1)}</td>)}</tr>
          <tr><td title="ship_date · 억">출하</td>{hs.map((h) => <td key={h.month}>{eok(h.shipment_krw, 1)}</td>)}</tr>
          <tr><td title="WATCH: 0.85 미만 2개월">단월 수주/출하</td>{hs.map((h) => <td key={h.month} className={h.single_ratio && h.single_ratio < 0.85 ? "bad" : ""}>{ratio(h.single_ratio)}</td>)}</tr>
          <tr><td title="AT_RISK: 0.8 미만">3개월 수주/출하</td>{hs.map((h) => <td key={h.month} className={h.rolling_ratio && h.rolling_ratio < 0.8 ? "bad" : ""}>{ratio(h.rolling_ratio)}</td>)}</tr>
          <tr><td title="AT_RISK: −15% 미만">3개월 수주 YoY</td>{hs.map((h) => <td key={h.month} className={h.has_yoy && h.yoy_pct < -15 ? "bad" : ""}>{h.has_yoy ? pct(h.yoy_pct) : "기록 부족"}</td>)}</tr>
          <tr className="row-strong"><td>상태</td>{hs.map((h) => <td key={h.month}><span className={`pill churn-${h.status}`}>{h.status}</span></td>)}</tr>
        </tbody>
      </table>
    </div>
  );
}

export function DecisionsView({ v }: { v: View }) {
  const ds = v.state.decisions ?? [];
  return (
    <Section title="승인 결정" mode={["mock", "real"]} note="승인권자 회신은 모의입니다. 결정은 승인 당시 제안 내용에 연결되며, 내용이 바뀌면 '낡음'으로 표시되어 다시 상신해야 합니다(R15).">
      {ds.length === 0 && <p className="muted small">현재 승인 결정이 없습니다. 승인권자가 결정하면(E2) 여기에 표시됩니다.</p>}
      {ds.map((d) => (
        <div key={d.decision_id} className="decision">
          <div>
            <b>{d.decision_id}</b> {d.approver_role}{d.entered_by && d.entered_by !== d.approver_role ? ` (대행 입력 ${d.entered_by}, 모의)` : ""} · {d.decided_at} · 참여 <b>{d.participation}</b>{" "}
            <span className={`pill pill-${slug(d.freshness)}`}>{d.freshness}</span>
          </div>
          <div className="small">
            {Object.entries(d.options ?? {}).map(([k, val]) => `${k} ${val}`).join(" · ")}
            {d.price_floor !== null && ` · 단가 인하 하한 ${pct(d.price_floor * 100)}`}
          </div>
          <ul className="conds">
            {(d.conditions ?? []).map((c) => <li key={c}><code>{c}</code> {v.condition_codes[c]}</li>)}
          </ul>
          {d.note && <div className="muted small">{d.note}</div>}
        </div>
      ))}
    </Section>
  );
}

export function TasksView({ v }: { v: View }) {
  const ts = v.state.tasks ?? [];
  const open = ts.filter((t) => t.state === "열림");
  const done = ts.filter((t) => t.state !== "열림");
  return (
    <Section title={`작업함 (열림 ${open.length})`} mode={["real"]}>
      {open.length === 0 && (
        <EmptyState compact swan="done" title={ts.length ? "현재 열린 작업이 없습니다" : "현재 작업이 없습니다"}>
          결정·제출 같은 사건이 처리되면 작업이 생성되어 여기에 표시됩니다. 원인 사건 번호와 담당 부서가 함께 기록됩니다.
        </EmptyState>
      )}
      {open.length > 0 && <TaskTable ts={open} />}
      {done.length > 0 && (
        <details className="dfold">
          <summary>완료 {done.length}건</summary>
          <TaskTable ts={done} done />
        </details>
      )}
    </Section>
  );
}

// 작업 4열 표: 담당 | 작업 | 기한 | 원인 사건(밀집도 2단계, 이슈 #71)
function TaskTable({ ts, done }: { ts: Task[]; done?: boolean }) {
  return (
    <table className={`grid text task-table${done ? " is-done" : ""}`}>
      <thead>
        <tr><th className="c-fit">담당</th><th className="c-text">작업</th><th className="c-fit">기한</th><th className="c-fit">원인 사건</th></tr>
      </thead>
      <tbody>
        {ts.map((t) => (
          <tr key={t.task_id} title={done ? `${t.task_id} · ${t.state}` : t.task_id}>
            <td className="c-fit"><span className="role">{t.owner_role}</span></td>
            <td className="c-text">{t.title}</td>
            <td className="c-fit">{t.due_date || "-"}</td>
            <td className="c-fit">사건 #{t.cause_seq}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

export function LogView({ v, onFork, busy }: { v: View; onFork?: (seq: number) => void; busy?: boolean }) {
  const log = [...(v.state.log ?? [])].reverse();
  // 이어지는 같은 업무일 사건을 한 머리글 아래로 묶는다(밀집도 2단계, 이슈 #71)
  const days: { date: string; entries: LogEntry[] }[] = [];
  for (const e of log) {
    const last = days[days.length - 1];
    if (last && last.date === e.event.sim_date) last.entries.push(e);
    else days.push({ date: e.event.sim_date, entries: [e] });
  }
  return (
    <Section title={`사건 로그 (${log.length}) · ${v.session.name}`} mode={["real"]} note="상태는 이 로그를 처음부터 다시 적용해 생성합니다. 규칙에 맞지 않는 사건은 기록되지 않습니다. '여기서 분기'를 누르면 해당 사건까지 복사한 시뮬레이션 세션이 생성됩니다(현재 세션은 변경되지 않음).">
      {log.length === 0 && <p className="muted small">현재 기록된 사건이 없습니다. 왼쪽에서 사건을 입력하거나 위의 시연 버튼을 눌러 주세요.</p>}
      {days.map((d) => (
        <section key={`${d.date}-${d.entries[0].event.seq}`} className="log-day">
          <h3 className="log-date">업무일 {d.date} <span className="muted small">사건 {d.entries.length}건</span></h3>
          <ol className="log">
            {d.entries.map((e: LogEntry) => (
              <li key={e.event.seq}>
                <div className="log-head">
                  <b>#{e.event.seq} {EVENT_LABEL[e.event.type] ?? e.event.type}</b>
                  <span className="muted small">
                    권한 {e.event.actor_role}{e.event.by && e.event.by !== e.event.actor_role ? ` · 대행 입력 ${e.event.by}` : e.event.by ? "" : " · 내부 실행"}
                  </span>
                  {onFork && <button className="ghost small log-fork" disabled={busy} onClick={() => onFork(e.event.seq)}>여기서 분기</button>}
                </div>
                <LogChanges changes={e.changes ?? []} />
              </li>
            ))}
          </ol>
        </section>
      ))}
    </Section>
  );
}

// 변경은 2줄까지 보이고 나머지는 "변경 N개 더 보기"로 펼친다.
function LogChanges({ changes }: { changes: string[] }) {
  const [open, setOpen] = useState(false);
  const shown = open ? changes : changes.slice(0, 2);
  const rest = changes.length - 2;
  if (changes.length === 0) return null;
  return (
    <>
      <ul className="changes">{shown.map((c, i) => <li key={i}>{c}</li>)}</ul>
      {rest > 0 && (
        <button type="button" className="note-more log-more" aria-expanded={open} onClick={() => setOpen(!open)}>
          {open ? "접기" : `변경 ${rest}개 더 보기`}
        </button>
      )}
    </>
  );
}

// 설계 단계·보류 목록(이전 안내 문단을 항목으로 나눔)
const DESIGN_ITEMS = [
  "LLM으로 자유 문장(RFP·회신) 필드화(현재 제외, 결합 지점은 추후 결정)",
  "W01 Masil 수요 변경(E6) 연결",
  "손실부담·변동대가·리콜 노출(R10·R11·R17)",
  "기존 고객 납기 변경 승인(영업 임원) 흐름",
  "CR-10·CR-11 회신의 계산 반영(PL-01 타 라인 공급, 확정 납기로 추정 납기 대체)",
];
const HOLD_ITEMS = [
  "M-B 원료 제약(PL-02 → M-B → V-205 독점 공급, C-201 더마 증량분의 원료 가용성)",
  "Masil 과거 수요 추정(재고 보충 규칙 4주 커버로 2026-06-22 전 기준일의 Masil 보호량 추정)",
];

// 가정과 출처: 성격별 필터(화면 안 상태), 값은 2줄까지(전체는 title), 설계·보류 목록은 접는다(밀집도 2단계, 이슈 #71)
export function AssumptionsView({ v }: { v: View }) {
  const [pick, setPick] = useState("전체");
  const labels = [...new Set(v.assumptions.map((a) => a.label))];
  const cur = labels.includes(pick) ? pick : "전체";
  const rows = cur === "전체" ? v.assumptions : v.assumptions.filter((a) => a.label === cur);
  const count = (l: string) => (l === "전체" ? v.assumptions.length : v.assumptions.filter((a) => a.label === l).length);
  return (
    <Section title="가정과 출처" mode={["real"]} note="원본 제약(능력·계약·승인권한)은 가정으로 없애지 않습니다. 회신이 오면 '회신 반영'으로 바뀝니다.">
      <div className="chips assume-filter" role="group" aria-label="성격 필터">
        {["전체", ...labels].map((l) => (
          <button key={l} type="button" className={`chip${cur === l ? " strong" : ""}`} aria-pressed={cur === l} onClick={() => setPick(l)}>
            {l} {count(l)}
          </button>
        ))}
      </div>
      <table className="grid text">
        <thead><tr><th className="c-fit">ID</th><th className="c-fit">성격</th><th className="c-text-s">항목</th><th className="c-text">지금 쓰는 값</th><th className="c-text-s">영향(쓰는 곳)</th></tr></thead>
        <tbody>
          {rows.map((a) => (
            <tr key={a.id}>
              <td className="c-fit">{a.id}</td>
              <td className="c-fit"><span className={`tag tag-${slug(a.label)}`}>{a.label}</span></td>
              <td className="c-text-s">{a.text}</td>
              <td className="c-text" title={a.value}><div className="cell-clamp2">{a.value}</div></td>
              <td className="c-text-s small">{a.affects}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <details className="dfold">
        <summary>설계 단계(구현 안 함) {DESIGN_ITEMS.length}건</summary>
        <ul className="dfold-list">{DESIGN_ITEMS.map((t) => <li key={t}>{t}</li>)}</ul>
      </details>
      <details className="dfold">
        <summary>보류(이번 마감 범위 밖, 11/13 결정) {HOLD_ITEMS.length}건</summary>
        <ul className="dfold-list">{HOLD_ITEMS.map((t) => <li key={t}>{t}</li>)}</ul>
      </details>
    </Section>
  );
}
