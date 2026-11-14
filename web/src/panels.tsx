import { useLayoutEffect, useRef, useState, type ReactNode } from "react";
import type { View, ProductEval, LineEval, OptionEval, LogEntry } from "./types";
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
  return (
    <section className="card">
      <header className="card-head">
        <h2>{title}</h2>
        {special && <span className="modes">{mode.map((m) => <Mode key={m} kind={m} />)}</span>}
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
  return (
    <Section
      title="원본 간 규모 차이"
      mode={["real"]}
      note={
        <>
          <span className="tag tag-가정">원본 간 불일치</span> 엔진의 여유·C-201 가용·제안 일정은 <b>{snapSource(v)}</b>를 사용합니다(D6). 생산 라인 보드의 월 생산·부하·상태는 <b>{batchSource(v)}</b>와 진행 주문 기록({b.order_file}, 기록 ~{b.cutoff})을 사용합니다. 두 원본은 capacity_qty를 연간·월간 어느 쪽으로 읽어도 실적과 스냅샷 가동률이 함께 맞지 않습니다. A05 알려진 한계 목록에 없는 불일치이므로, 판단은 스냅샷 기준으로 하고 배치 실적은 라인 보드의 비율(부하 ÷ 실적)에만 사용합니다.
        </>
      }
    >
      <div className="scroll">
        <table className="grid">
          <thead>
            <tr>
              <th>라인</th><th>제품군</th>
              <ColHead main="월 생산" sub={`배치 실적 · EA/월 · ${b.t12_from}~${b.t12_to} 평균`} />
              <ColHead main="capacity_qty" sub={`능력 스냅샷 ${b.snapshot_date} · 단위 미기재`} />
              <ColHead main="스냅샷 가동률" sub="능력 스냅샷" />
              <ColHead main="연간으로 읽으면: 월 능력" sub="capacity ÷ 12 · EA/월" />
              <ColHead main="실적 ÷ 월 능력" sub="연간 해석(엔진 기본, D6)" />
              <ColHead main="월간으로 읽으면: 실적 ÷ capacity" sub="월간 해석(CR-01 대안)" />
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
        <table className="grid">
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
  const sides = [c.base, c.alt];
  const months = c.months ?? [];
  const diff = (a: number, b: number) => (b - a === 0 ? "-" : `${b - a > 0 ? "+" : ""}${ea(b - a)}`);
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
        <table className="grid">
          <thead>
            <tr>
              <th>시나리오</th><th>OPT-01 결정</th><th>설치 완료</th><th>적격(재검증)</th><th>산입 시작</th><th>최초 반영 월(계약 안)</th>
              <th>OPT-01 공급 합</th><th>PL-02 제안 합</th><th>부족 합</th><th>공급률</th><th>지연분 노출</th><th>계약 총액 기준</th>
            </tr>
          </thead>
          <tbody>
            {sides.map((s, i) => (
              <tr key={s.label} className={i === 0 ? "row-strong" : ""}>
                <td>{s.label}</td>
                <td>{s.status}{s.conditional && s.usable ? " (조건부)" : ""}</td>
                <td>{s.install_date}</td>
                <td>{s.qualified_date} ({s.validation_weeks}주)</td>
                <td>{s.usable ? `${s.start_month}~` : "반영 안 함"}</td>
                <td>{s.first_supply_month || "없음"}</td>
                <td>{ea(s.opt_qty_total)}</td>
                <td>{ea(s.schedule_total)}</td>
                <td>{ea(s.short_total)}</td>
                <td>{pct(s.coverage_pct)}</td>
                <td>{eok(s.penalty_backlog_krw)}</td>
                <td>{eok(s.penalty_contract_krw)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="scroll">
        <table className="grid">
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
            <tr><td>전체 공급 · 기본</td>{months.map((m) => <td key={m.month}>{ea(m.base_supply)}</td>)}</tr>
            <tr><td>전체 공급 · 비교</td>{months.map((m) => <td key={m.month}>{ea(m.alt_supply)}</td>)}</tr>
            <tr className="row-strong"><td>제안 · 기본</td>{months.map((m) => <td key={m.month}>{ea(m.base_schedule)}</td>)}</tr>
            <tr className="row-strong"><td>제안 · 비교</td>{months.map((m) => <td key={m.month}>{ea(m.alt_schedule)}</td>)}</tr>
            <tr><td>제안 차이(비교 − 기본)</td>{months.map((m) => <td key={m.month} className={m.alt_schedule !== m.base_schedule ? "warn" : "muted"}>{diff(m.base_schedule, m.alt_schedule)}</td>)}</tr>
            <tr className="row-bad"><td>부족 · 기본 / 비교</td>{months.map((m) => <td key={m.month}>{m.base_short || m.alt_short ? `${ea(m.base_short)} / ${ea(m.alt_short)}` : "-"}</td>)}</tr>
          </tbody>
        </table>
      </div>
    </Section>
  );
}

// 라인 여유(R1)와 증설·전환안 적격 능력일(R3)
export function CapacityView({ v }: { v: View }) {
  const ev = v.state.evaluation;
  return (
    <Section title="라인 여유와 증설·전환안" mode={["real"]} note={`이 표는 엔진 판단 기준입니다. 출처: ${snapSource(v)}(단위 ${v.state.scenario.unit_confirmed ? "확인됨" : "미확인"}). 여유 = 능력 × (1 − 가동률). F1 가동률 가중 ${pct(ev.f1_weighted_util, 2)} / 단순 ${pct(ev.f1_simple_util, 2)}. 생산 라인 보드의 월 생산(배치 실적)과는 규모가 다릅니다. 아래 '원본 간 규모 차이'를 확인해 주세요.`}>
      <div className="scroll">
        <table className="grid">
          <thead>
            <tr>
              <th>라인</th><th className="c-left">공장</th><th className="c-left">제품군</th>
              <ColHead main="능력" sub={`스냅샷 ${v.data_basis.snapshot_date} · EA/년(D6)`} />
              <ColHead main="가동률" sub="스냅샷" />
              <ColHead main="기본 여유" sub="계산 · EA/년" />
              <ColHead main="Masil 보호" sub={`발주 ~${v.data_basis.cutoff} · EA`} />
              <ColHead main="신규 수주" sub="E5 · EA/년" />
              <ColHead main="기준일 뒤 신규 수주" sub={`원본 시계 · ${v.state.scenario.hist_basis === "gross" ? "총량(모의)" : "순증분"} · EA`} />
              <ColHead main="C-201 가용" sub="계산 · EA/년" />
              <ColHead main="월" sub="÷ 12 · EA/월" />
            </tr>
          </thead>
          <tbody>
            {ev.lines.map((l: LineEval) => {
              const c201 = l.product_id === "PL-01" || l.product_id === "PL-02";
              return (
                <tr key={l.line_id} className={c201 ? "" : "muted"}>
                  <td>{l.line_id}</td><td className="c-left">{l.plant_id}</td><td className="c-left">{l.product_id}{l.dedicated ? " (F1 전용)" : ""}</td>
                  <td>{ea(l.capacity_qty)}</td><td>{pct(l.utilization_pct, 0)}</td><td>{ea(l.spare_annual)}</td>
                  <td>{c201 ? ea(l.masil_protected) : ""}{l.masil_deficit > 0 && <span className="bad"> (부족 {ea(l.masil_deficit)})</span>}</td>
                  <td>{c201 ? ea(l.new_orders) : ""}</td><td className={l.hist_orders ? "warn" : ""}>{c201 ? ea(l.hist_orders) : ""}</td><td>{c201 ? ea(l.base_avail_annual) : "C-201 대상 아님"}</td><td>{c201 ? ea(l.base_monthly) : ""}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      <TableSrc>능력·가동률 = 능력 스냅샷 {v.data_basis.snapshot_date}(EA/년, D6) · 기본 여유·C-201 가용·월 = 계산 · Masil 보호 = 발주 ~{v.data_basis.cutoff} · 신규 수주 = E5 · 기준일 뒤 신규 수주 = 원본 시계</TableSrc>
      <div className="scroll">
        <table className="grid">
          <thead>
            <tr>
              <th>안</th><th className="c-left">대상(매핑)</th><th>투자</th><th>추가(연)</th><th className="c-left">결정</th><th>결정일</th><th>설치</th><th>적격(재검증 {ev.options[0]?.validation_weeks}주)</th><th>산입</th><th className="c-left">선행조건</th><th className="c-left">단계</th><th>회수</th>
            </tr>
          </thead>
          <tbody>
            {ev.options.map((o: OptionEval) => (
              <tr key={o.option_id} className={o.usable ? "" : "muted"}>
                <td>{o.option_id} {o.type}</td><td className="c-left" title={o.target_line_text}>{o.line_id} <span className="tag">합성 매핑</span></td>
                <td>{eok(o.capex_krw, 0)}</td><td>{ea(o.added_annual_qty)}</td><td className="c-left">{o.status}</td>
                <td>{o.decision_date}{o.decision_date_assumed ? " (가정)" : ""}</td><td>{o.install_date}</td><td>{o.qualified_date}</td>
                <td>{o.start_month}~{o.option_id === "OPT-01" && ev.compare_opt01 && <div className="muted small">비교(재검증 0주): {ev.compare_opt01.alt.start_month}~</div>}</td>
                <td className="c-left">{(o.pending_requests ?? []).join(", ") || "-"}</td><td className="c-left">{o.phase}</td><td>{o.payback_months.toFixed(1)}개월</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
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
          <table className="grid">
            <thead><tr><th>제품군</th><th>건수</th><th>수량</th><th>7월 추정 납품</th><th>8월 추정 납품</th><ColHead main="월 생산" sub={batchSource(v)} /></tr></thead>
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
        <div className="chips">
          {res.map((r) => <span key={r.product_id + r.month} className="chip">{r.product_id} {r.month} {ea(r.qty)} ({r.basis})</span>)}
        </div>
      )}
    </Section>
  );
}

// 기존 고객 이탈 신호(R12)와 반환 금지(R13): 수주(order_date)와 출하(ship_date)를 나눠 본다(W05 단초).
export function ChurnView({ v }: { v: View }) {
  const sigs = v.state.evaluation.churn ?? [];
  const sc = v.state.scenario;
  const ratio = (x: number) => (x ? x.toFixed(3) : "-");
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
      {sigs.map((s) => (
        <div key={s.customer_id} className="product">
          <div className="product-head">
            <b>{s.customer_id}</b>
            <span className="muted small">매출 비중 {s.revenue_share_pct}% · 진행 주문 {ea(s.open_orders)}건(PL-01 {ea(s.open_qty?.["PL-01"])} · PL-02 {ea(s.open_qty?.["PL-02"])} EA){s.status === "AT_RISK" ? " → 검토(R13), 보호 유지" : ""}</span>
            <span className={`pill churn-${s.status}`}>{s.status}{s.since ? ` (${s.since}~)` : ""}</span>
          </div>
          <div className="scroll">
            <table className="grid">
              <thead>
                <tr><th>월</th>{(s.history ?? []).map((h) => <th key={h.month}>{h.month.slice(2)}</th>)}</tr>
              </thead>
              <tbody>
                <tr><td title="order_date · 억">수주</td>{(s.history ?? []).map((h) => <td key={h.month}>{eok(h.booking_krw, 1)}</td>)}</tr>
                <tr><td title="ship_date · 억">출하</td>{(s.history ?? []).map((h) => <td key={h.month}>{eok(h.shipment_krw, 1)}</td>)}</tr>
                <tr><td title="WATCH: 0.85 미만 2개월">단월 수주/출하</td>{(s.history ?? []).map((h) => <td key={h.month} className={h.single_ratio && h.single_ratio < 0.85 ? "bad" : ""}>{ratio(h.single_ratio)}</td>)}</tr>
                <tr><td title="AT_RISK: 0.8 미만">3개월 수주/출하</td>{(s.history ?? []).map((h) => <td key={h.month} className={h.rolling_ratio && h.rolling_ratio < 0.8 ? "bad" : ""}>{ratio(h.rolling_ratio)}</td>)}</tr>
                <tr><td title="AT_RISK: −15% 미만">3개월 수주 YoY</td>{(s.history ?? []).map((h) => <td key={h.month} className={h.has_yoy && h.yoy_pct < -15 ? "bad" : ""}>{h.has_yoy ? pct(h.yoy_pct) : "기록 부족"}</td>)}</tr>
                <tr className="row-strong"><td>상태</td>{(s.history ?? []).map((h) => <td key={h.month}><span className={`pill churn-${h.status}`}>{h.status}</span></td>)}</tr>
              </tbody>
            </table>
          </div>
          <TableSrc>단위 억 · 수주 = order_date, 출하 = ship_date · WATCH = 단월 수주/출하 0.85 미만 2개월 · AT_RISK = 3개월 수주/출하 0.8 미만 또는 YoY −15% 미만(A-23)</TableSrc>
        </div>
      ))}
      {sigs.length === 0 && <p className="muted small">현재 매출 비중 10% 이상인 대상 고객이 없습니다.</p>}
    </Section>
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
      <ul className="tasks">
        {open.map((t) => (
          <li key={t.task_id}><span className="role">{t.owner_role}</span> {t.title} <span className="muted small">{t.due_date ? `~${t.due_date}` : ""} · 사건 #{t.cause_seq}</span></li>
        ))}
      </ul>
      {done.length > 0 && (
        <details>
          <summary className="muted small">완료 {done.length}건</summary>
          <ul className="tasks done">{done.map((t) => <li key={t.task_id}><span className="role">{t.owner_role}</span> {t.title}</li>)}</ul>
        </details>
      )}
    </Section>
  );
}

export function LogView({ v, onFork, busy }: { v: View; onFork?: (seq: number) => void; busy?: boolean }) {
  const log = [...(v.state.log ?? [])].reverse();
  return (
    <Section title={`사건 로그 (${log.length}) · ${v.session.name}`} mode={["real"]} note="상태는 이 로그를 처음부터 다시 적용해 생성합니다. 규칙에 맞지 않는 사건은 기록되지 않습니다. '여기서 분기'를 누르면 해당 사건까지 복사한 시뮬레이션 세션이 생성됩니다(현재 세션은 변경되지 않음).">
      {log.length === 0 && <p className="muted small">현재 기록된 사건이 없습니다. 왼쪽에서 사건을 입력하거나 위의 시연 버튼을 눌러 주세요.</p>}
      <ol className="log">
        {log.map((e: LogEntry) => (
          <li key={e.event.seq}>
            <div>
              <b>#{e.event.seq} {EVENT_LABEL[e.event.type] ?? e.event.type}</b>{" "}
              <span className="muted small">
                권한 {e.event.actor_role}{e.event.by && e.event.by !== e.event.actor_role ? ` · 대행 입력 ${e.event.by}` : e.event.by ? "" : " · 내부 실행"} · 업무일 {e.event.sim_date}
              </span>
              {onFork && <button className="ghost small" disabled={busy} onClick={() => onFork(e.event.seq)}>여기서 분기</button>}
            </div>
            <ul className="changes">{(e.changes ?? []).map((c, i) => <li key={i}>{c}</li>)}</ul>
          </li>
        ))}
      </ol>
    </Section>
  );
}

export function AssumptionsView({ v }: { v: View }) {
  return (
    <Section title="가정과 출처" mode={["real"]} note="원본 제약(능력·계약·승인권한)은 가정으로 없애지 않습니다. 회신이 오면 '회신 반영'으로 바뀝니다.">
      <table className="grid text">
        <thead><tr><th className="c-fit">ID</th><th className="c-fit">성격</th><th className="c-text-s">항목</th><th className="c-text">지금 쓰는 값</th><th className="c-text-s">영향(쓰는 곳)</th></tr></thead>
        <tbody>
          {v.assumptions.map((a) => (
            <tr key={a.id}><td className="c-fit">{a.id}</td><td className="c-fit"><span className={`tag tag-${slug(a.label)}`}>{a.label}</span></td><td className="c-text-s">{a.text}</td><td className="c-text">{a.value}</td><td className="c-text-s small">{a.affects}</td></tr>
          ))}
        </tbody>
      </table>
      <p className="muted small">설계 단계(구현 안 함): LLM으로 자유 문장(RFP·회신) 필드화(현재 제외, 결합 지점은 추후 결정), W01 Masil 수요 변경(E6) 연결, 손실부담·변동대가·리콜 노출(R10·R11·R17), 기존 고객 납기 변경 승인(영업 임원) 흐름, CR-10·CR-11 회신의 계산 반영(PL-01 타 라인 공급, 확정 납기로 추정 납기 대체).</p>
      <p className="muted small">보류(이번 마감 범위 밖, 11/13 결정): M-B 원료 제약(PL-02 → M-B → V-205 독점 공급, C-201 더마 증량분의 원료 가용성), Masil 과거 수요 추정(재고 보충 규칙 4주 커버로 2026-06-22 전 기준일의 Masil 보호량 추정).</p>
    </Section>
  );
}
