import type { ReactNode } from "react";
import type { View, ProductEval, LineEval, OptionEval, LogEntry } from "./types";
import { BASIS_LABEL, EVENT_LABEL, PRODUCT_LABEL, ea, eok, pct, slug, won } from "./format";

// 화면마다 실제 작동 / 모의 / 설계 단계를 표시한다(A04).
export function Mode({ kind }: { kind: "real" | "mock" | "design" }) {
  const text = { real: "실제 작동", mock: "모의", design: "설계 단계" }[kind];
  return <span className={`mode mode-${kind}`}>{text}</span>;
}

export function Section({ title, mode, children, note }: { title: string; mode: ("real" | "mock" | "design")[]; children: ReactNode; note?: ReactNode }) {
  return (
    <section className="card">
      <header className="card-head">
        <h2>{title}</h2>
        <span className="modes">{mode.map((m) => <Mode key={m} kind={m} />)}</span>
      </header>
      {note && <p className="muted small">{note}</p>}
      {children}
    </section>
  );
}

// 수치 출처 표시(이슈 #27): 엔진은 능력 스냅샷, 생산 라인 보드는 배치 실적을 쓴다. 표의 열 제목 아래에 출처·단위·기간을 붙인다.
export function ColHead({ main, sub }: { main: ReactNode; sub: string }) {
  return (
    <th>
      {main}
      <span className="th-sub">{sub}</span>
    </th>
  );
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
          <span className="tag tag-가정">원본 간 불일치</span> 엔진의 여유·C-201 가용·제안 일정은 <b>{snapSource(v)}</b>를 쓴다(D6). 생산 라인 보드의 월 생산·부하·상태는 <b>{batchSource(v)}</b>와 진행 주문 기록({b.order_file}, 기록 ~{b.cutoff})을 쓴다. 두 원본은 capacity_qty를 연간으로 읽어도 월간으로 읽어도 실적과 스냅샷 가동률이 함께 맞지 않는다. A05 알려진 한계 목록에 없는 불일치라, 판단은 스냅샷으로 하고 배치 실적은 라인 보드의 비율(부하 ÷ 실적)로만 쓴다.
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
      <p className="muted small">빨간 칸: 연간 해석이면 실적이 능력을 넘고(100% 초과), 월간 해석이면 실적 비율이 스냅샷 가동률과 10%p 넘게 다르다. 단위는 생산관리 확인 요청(CR-01)으로 묻는다.</p>
    </Section>
  );
}

const LEVEL_ORDER = { 차단: 0, 위험: 1, 미확인: 2 } as const;

// 판단 카드: 엔진 수치 + 근거(규칙) + 미확인. 결정은 사람이 한다.
export function JudgmentCard({ v }: { v: View }) {
  const s = v.state;
  const ev = s.evaluation;
  const p1 = ev.products["PL-01"];
  const p2 = ev.products["PL-02"];
  const flags = [...(ev.flags ?? [])].sort((a, b) => LEVEL_ORDER[a.level] - LEVEL_ORDER[b.level]);
  const l1 = s.lines?.find((l) => l.product_id === "PL-01");
  const l2 = s.lines?.find((l) => l.product_id === "PL-02");
  const last = s.log?.[s.log.length - 1];
  return (
    <Section title="판단 카드" mode={["real"]} note={`숫자는 결정 모델(엔진)이 원본에서 다시 계산하고, 승인은 권한자가 한다. 입력은 고정 필드만 쓴다(LLM 현재 제외). 여유·공급 출처: ${snapSource(v)}. 생산 라인 보드의 배치 실적과는 규모가 다르다('여유·증설안' 탭의 원본 간 규모 차이).`}>
      <div className="kpis">
        <Kpi label="PL-02 더마 제안 가능" value={`${ea(p2?.schedule_total)} EA`} sub={`요구 ${ea((p2?.monthly_demand ?? 0) * ev.scenario.contract_months)} 중 ${pct(p2?.coverage_pct)} · ${l2?.qty_status ?? "접수 전"} · 계약 ${ev.scenario.contract_months}개월 합계, 능력 스냅샷 기준`} tone={p2 && p2.coverage_pct < 100 ? "warn" : "ok"} />
        <Kpi label="PL-01 기초" value={l1?.qty_status === "미확정" || !l1 ? "수량 미확정" : `${ea(p1?.schedule_total)} EA`} sub={`확인 전 상한 월 ${ea(p1?.months?.[0]?.supply)} EA(능력 스냅샷 여유 ÷ 12) · 요구 월 ${ea(p1?.monthly_demand)}`} tone="bad" />
        <Kpi label="전량 약속 시 지체상금(지연분)" value={eok((p1?.penalty_backlog_krw ?? 0) + (p2?.penalty_backlog_krw ?? 0))} sub={`계약 총액 기준이면 ${eok(ev.penalty_contract_krw)} (월 ${eok(ev.penalty_contract_monthly_krw)})`} tone="bad" />
        <Kpi label="연 공헌이익(요구 전량, 원료비 기준)" value={eok((p1?.contrib_request_krw ?? 0) + (p2?.contrib_request_krw ?? 0), 1)} sub={`램프 제안 기준 ${eok((p1?.contrib_schedule_krw ?? 0) + (p2?.contrib_schedule_krw ?? 0), 1)} · 단가 인하 ${pct(ev.scenario.price_cut * 100)}`} tone="ok" />
      </div>
      <h3>미확인·위험·차단 ({flags.length})</h3>
      <ul className="flags">
        {flags.map((f) => (
          <li key={f.code} className={`flag flag-${f.level}`}>
            <span className="flag-level">{f.level}</span>
            <span className="rule">{f.rule}</span> {f.message}
          </li>
        ))}
      </ul>
      {last && (
        <>
          <h3>
            직전 사건의 변화
          </h3>
          <p className="small">
            <b>{EVENT_LABEL[last.event.type] ?? last.event.type}</b> — {(last.changes ?? []).join(" · ")}
          </p>
        </>
      )}
      {s.last_blocks && s.last_blocks.length > 0 && (
        <p className="blocked">제출 차단: {s.last_blocks.join(" / ")}</p>
      )}
    </Section>
  );
}

function Kpi({ label, value, sub, tone }: { label: string; value: string; sub: string; tone: "ok" | "warn" | "bad" }) {
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
    <Section title="제안 일정: 라인×월 공급과 부족" mode={["real"]} note={`납품 개시 ${s.scenario.delivery_start}${s.scenario.start_confirmed ? "(바이어 확인)" : "(가정 A-04)"} · 계약 ${s.scenario.contract_months}개월 · 증설·전환 능력은 적격일 다음 달부터 산입(R3). 점선 = 조건부 공급. 단위 EA/월. '기존' 공급 = 능력 스냅샷 여유(Masil 보호 뒤) ÷ 12, OPT = strat_capex_plan_cv 추가 능력(연간) ÷ 12.`}>
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
          <span className="tag tag-가정">대안 시나리오 · 제출 기준 아님</span> 기본은 OPT-01에도 설치 뒤 적격 기간(A-08)을 더한다. 비교는 원본 {c.source}을 적용해 적격 기간 0주로 계산한다. 결정·회신·납품 개시월·OPT-02는 기본과 같다. PL-02 수량, 단위 EA/월.
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
    <Section title="라인 여유와 증설·전환안" mode={["real"]} note={`이 표는 엔진 판단 기준이다. 출처: ${snapSource(v)}(단위 ${v.state.scenario.unit_confirmed ? "확인됨" : "미확인"}). 여유 = 능력 × (1 − 가동률). F1 가동률 가중 ${pct(ev.f1_weighted_util, 2)} / 단순 ${pct(ev.f1_simple_util, 2)}. 생산 라인 보드의 월 생산(배치 실적)과는 규모가 다르다 → 아래 '원본 간 규모 차이'.`}>
      <div className="scroll">
        <table className="grid">
          <thead>
            <tr>
              <th>라인</th><th>공장</th><th>제품군</th>
              <ColHead main="능력" sub={`스냅샷 ${v.data_basis.snapshot_date} · EA/년(D6)`} />
              <ColHead main="가동률" sub="스냅샷" />
              <ColHead main="기본 여유" sub="계산 · EA/년" />
              <ColHead main="Masil 보호" sub={`발주 ~${v.data_basis.cutoff} · EA`} />
              <ColHead main="신규 수주" sub="E5 · EA/년" />
              <ColHead main="C-201 가용" sub="계산 · EA/년" />
              <ColHead main="월" sub="÷ 12 · EA/월" />
            </tr>
          </thead>
          <tbody>
            {ev.lines.map((l: LineEval) => {
              const c201 = l.product_id === "PL-01" || l.product_id === "PL-02";
              return (
                <tr key={l.line_id} className={c201 ? "" : "muted"}>
                  <td>{l.line_id}</td><td>{l.plant_id}</td><td>{l.product_id}{l.dedicated ? " (F1 전용)" : ""}</td>
                  <td>{ea(l.capacity_qty)}</td><td>{pct(l.utilization_pct, 0)}</td><td>{ea(l.spare_annual)}</td>
                  <td>{c201 ? ea(l.masil_protected) : ""}{l.masil_deficit > 0 && <span className="bad"> (부족 {ea(l.masil_deficit)})</span>}</td>
                  <td>{c201 ? ea(l.new_orders) : ""}</td><td>{c201 ? ea(l.base_avail_annual) : "C-201 대상 아님"}</td><td>{c201 ? ea(l.base_monthly) : ""}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      <div className="scroll">
        <table className="grid">
          <thead>
            <tr>
              <th>안</th><th>대상(매핑)</th><th>투자</th><th>추가(연)</th><th>결정</th><th>결정일</th><th>설치</th><th>적격(재검증 {ev.options[0]?.validation_weeks}주)</th><th>산입</th><th>선행조건</th><th>단계</th><th>회수</th>
            </tr>
          </thead>
          <tbody>
            {ev.options.map((o: OptionEval) => (
              <tr key={o.option_id} className={o.usable ? "" : "muted"}>
                <td>{o.option_id} {o.type}</td><td title={o.target_line_text}>{o.line_id} <span className="tag">합성 매핑</span></td>
                <td>{eok(o.capex_krw, 0)}</td><td>{ea(o.added_annual_qty)}</td><td>{o.status}</td>
                <td>{o.decision_date}{o.decision_date_assumed ? " (가정)" : ""}</td><td>{o.install_date}</td><td>{o.qualified_date}</td>
                <td>{o.start_month}~{o.option_id === "OPT-01" && ev.compare_opt01 && <div className="muted small">비교(재검증 0주): {ev.compare_opt01.alt.start_month}~</div>}</td>
                <td>{(o.pending_requests ?? []).join(", ") || "-"}</td><td>{o.phase}</td><td>{o.payback_months.toFixed(1)}개월</td>
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
    <Section title="약속 원장" mode={["real"]} note="기존 확정 약속은 C-201 제안이 밀 수 없다(R2). Masil 발주는 Corevia 수주장부 밖이라 합성 매핑으로 L-01·L-02에 붙인다.">
      <div className="cols">
        <div>
          <h3>진행 중 B2B 주문 (원본, 가동률 포함 가정 A-06)</h3>
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
          <p className="muted small">전체 {ea(c.in_progress_count)}건 · {ea(c.in_progress_qty)} EA. 추정 납기는 주문일 + 조건부 리드타임 중앙값(A-14, 검산용). 대리점 주문 {ea(v.data_basis.agency_orders)}건 · {ea(v.data_basis.agency_qty)} EA는 빠져 있다(A-17).</p>
        </div>
        <div>
          <h3>Masil 발주 (보호, Win 등급) <span className="tag">합성 매핑</span></h3>
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
      <h3>C-201 가예약 ({res.length}건)</h3>
      {res.length === 0 ? (
        <p className="muted small">제출(E7) 전에는 가예약이 없다.</p>
      ) : (
        <div className="chips">
          {res.map((r) => <span key={r.product_id + r.month} className="chip">{r.product_id} {r.month} {ea(r.qty)} ({r.basis})</span>)}
        </div>
      )}
    </Section>
  );
}

export function DecisionsView({ v }: { v: View }) {
  const ds = v.state.decisions ?? [];
  return (
    <Section title="승인 결정" mode={["mock", "real"]} note="승인권자 회신은 모의다. 결정은 승인 당시 제안 내용에 묶이고, 내용이 바뀌면 낡음이 되어 다시 상신해야 한다(R15).">
      {ds.length === 0 && <p className="muted small">결정 없음</p>}
      {ds.map((d) => (
        <div key={d.decision_id} className="decision">
          <div>
            <b>{d.decision_id}</b> {d.approver_role} · {d.decided_at} · 참여 <b>{d.participation}</b>{" "}
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

export function LogView({ v }: { v: View }) {
  const log = [...(v.state.log ?? [])].reverse();
  return (
    <Section title={`사건 로그 (${log.length})`} mode={["real"]} note="상태는 이 로그를 처음부터 다시 적용해 만든다. 규칙에 맞지 않는 사건은 기록되지 않는다.">
      {log.length === 0 && <p className="muted small">사건 없음. 왼쪽에서 사건을 넣거나 위의 시연 버튼을 누른다.</p>}
      <ol className="log">
        {log.map((e: LogEntry) => (
          <li key={e.event.seq}>
            <div>
              <b>#{e.event.seq} {EVENT_LABEL[e.event.type] ?? e.event.type}</b> <span className="muted small">{e.event.actor_role} · 업무일 {e.event.sim_date}</span>
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
    <Section title="가정과 출처" mode={["real"]} note="원본 제약(능력·계약·승인권한)은 가정으로 지우지 않는다. 회신이 오면 '회신 반영'으로 바뀐다.">
      <table className="grid text">
        <thead><tr><th>ID</th><th>항목</th><th>지금 쓰는 값</th><th>성격</th><th>영향(쓰는 곳)</th></tr></thead>
        <tbody>
          {v.assumptions.map((a) => (
            <tr key={a.id}><td className="nowrap">{a.id}</td><td>{a.text}</td><td>{a.value}</td><td><span className={`tag tag-${slug(a.label)}`}>{a.label}</span></td><td className="small">{a.affects}</td></tr>
          ))}
        </tbody>
      </table>
      <p className="muted small">설계 단계(구현 안 함): LLM으로 자유 문장(RFP·회신) 필드화(현재 제외, 결합 지점은 추후 결정), W01 Masil 수요 변경(E6) 연결, 손실부담·변동대가·리콜 노출(R10·R11·R17), 기존 고객 납기 변경 승인(영업 임원) 흐름.</p>
    </Section>
  );
}
