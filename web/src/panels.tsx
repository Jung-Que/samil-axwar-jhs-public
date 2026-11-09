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
    <Section title="판단 카드" mode={["real"]} note="숫자는 결정 모델(엔진)이 원본에서 다시 계산한다. AI는 숫자를 만들지 않고, 승인은 권한자가 한다.">
      <div className="kpis">
        <Kpi label="PL-02 더마 제안 가능" value={`${ea(p2?.schedule_total)} EA`} sub={`요구 ${ea((p2?.monthly_demand ?? 0) * ev.scenario.contract_months)} 중 ${pct(p2?.coverage_pct)} · ${l2?.qty_status ?? "접수 전"}`} tone={p2 && p2.coverage_pct < 100 ? "warn" : "ok"} />
        <Kpi label="PL-01 기초" value={l1?.qty_status === "미확정" || !l1 ? "수량 미확정" : `${ea(p1?.schedule_total)} EA`} sub={`확인 전 상한 월 ${ea(p1?.months?.[0]?.supply)} EA · 요구 월 ${ea(p1?.monthly_demand)}`} tone="bad" />
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
            직전 사건의 변화 <span className="muted small">설명은 결정 모델 템플릿(A3)</span>
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
    <Section title="제안 일정: 라인×월 공급과 부족" mode={["real"]} note={`납품 개시 ${s.scenario.delivery_start}${s.scenario.start_confirmed ? "(바이어 확인)" : "(가정 A-04)"} · 계약 ${s.scenario.contract_months}개월 · 증설·전환 능력은 적격일 다음 달부터 산입(R3). 점선 = 조건부 공급.`}>
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

// 라인 여유(R1)와 증설·전환안 적격 능력일(R3)
export function CapacityView({ v }: { v: View }) {
  const ev = v.state.evaluation;
  return (
    <Section title="라인 여유와 증설·전환안" mode={["real"]} note={`capacity_qty는 연간 EA로 읽는다(D6, 단위 ${v.state.scenario.unit_confirmed ? "확인됨" : "미확인"}). 여유 = 능력 × (1 − 가동률). F1 가동률 가중 ${pct(ev.f1_weighted_util, 2)} / 단순 ${pct(ev.f1_simple_util, 2)}.`}>
      <div className="scroll">
        <table className="grid">
          <thead>
            <tr>
              <th>라인</th><th>공장</th><th>제품군</th><th>능력</th><th>가동률</th><th>기본 여유(연)</th><th>Masil 보호</th><th>신규 수주</th><th>C-201 가용(연)</th><th>월</th>
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
                <td>{o.decision_date}{o.decision_date_assumed ? " (가정)" : ""}</td><td>{o.install_date}</td><td>{o.qualified_date}</td><td>{o.start_month}~</td>
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
            <thead><tr><th>제품군</th><th>건수</th><th>수량</th><th>7월 추정 납품</th><th>8월 추정 납품</th><th>월 생산(T12)</th></tr></thead>
            <tbody>
              {["PL-01", "PL-02"].map((p) => (
                <tr key={p}>
                  <td>{p}</td><td>{ea(c.count_by_product[p])}</td><td>{ea(c.by_product[p])}</td>
                  <td>{ea(c.est_due[p]?.["2026-07"])}</td><td>{ea(c.est_due[p]?.["2026-08"])}</td><td>{ea(c.t12_monthly_prod[p])}</td>
                </tr>
              ))}
            </tbody>
          </table>
          <p className="muted small">전체 {ea(c.in_progress_count)}건 · {ea(c.in_progress_qty)} EA. 추정 납기는 주문일 + 조건부 리드타임 중앙값(A-14, 검산용).</p>
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
              {e.event.ai_call && <AiBadge c={e.event.ai_call} />}
            </div>
            <ul className="changes">{(e.changes ?? []).map((c, i) => <li key={i}>{c}</li>)}</ul>
          </li>
        ))}
      </ol>
    </Section>
  );
}

export function AiBadge({ c }: { c: { mode: string; validation: string; human_edited: boolean; model: string } }) {
  const label = c.mode === "live" ? `실제 호출(${c.model})` : c.mode === "recorded" ? "녹화 응답 [모의]" : "AI 미사용";
  return (
    <span className={`ai ai-${c.mode}`}>
      A1 구조화 · {label} · 검증 {c.validation === "pass" ? "통과" : "실패"}{c.human_edited ? " · 사람이 수정" : " · 사람 확인"}
    </span>
  );
}

export function AssumptionsView({ v }: { v: View }) {
  return (
    <Section title="가정과 출처" mode={["real"]} note="원본 제약(능력·계약·승인권한)은 가정으로 지우지 않는다. 회신이 오면 '회신 반영'으로 바뀐다.">
      <table className="grid text">
        <thead><tr><th>ID</th><th>항목</th><th>지금 쓰는 값</th><th>성격</th></tr></thead>
        <tbody>
          {v.assumptions.map((a) => (
            <tr key={a.id}><td>{a.id}</td><td>{a.text}</td><td>{a.value}</td><td><span className={`tag tag-${slug(a.label)}`}>{a.label}</span></td></tr>
          ))}
        </tbody>
      </table>
      <p className="muted small">설계 단계(구현 안 함): W01 Masil 수요 변경(E6) 연결, 손실부담·변동대가·리콜 노출(R10·R11·R17), 기존 고객 납기 변경 승인(영업 임원) 흐름.</p>
    </Section>
  );
}
