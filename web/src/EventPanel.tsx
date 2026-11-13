import { useEffect, useState } from "react";
import { api } from "./api";
import type { ConfirmRequest, Department, View } from "./types";
import { Section } from "./panels";
import { EVENT_LABEL, slug } from "./format";
import { canEvent } from "./perm";

type Run = (fn: () => Promise<View>) => Promise<void>;
type Fields = Record<string, unknown>;

// 구조화된 고정 필드로 입력하는 사건. 자유 문장·LLM은 현재 제외(11/10 사용자 결정).
const FORM_KIND: Record<string, string> = { E1_RFP: "rfp", E2_DECISION: "decision", E4_BUYER_REPLY: "buyer_reply" };
const ACTOR: Record<string, string> = {
  E0_CLOCK: "시스템", E1_RFP: "영업", E2_DECISION: "CEO/이사회", E4_BUYER_REPLY: "영업", E5_NEW_ORDER: "영업", E7_SUBMIT_PROPOSAL: "영업", E8_SUBMIT_APPROVAL: "영업",
};

// 입찰 흐름에서 다음에 오는 사건 순서(E0 시계·E5 신규 수주는 언제나 가능하므로 맨 뒤).
const NEXT_ORDER = ["E1_RFP", "E8_SUBMIT_APPROVAL", "E2_DECISION", "E7_SUBMIT_PROPOSAL", "E4_BUYER_REPLY", "E0_CLOCK", "E5_NEW_ORDER"];

function available(v: View): Record<string, boolean> {
  const st = v.state.bid?.state;
  return {
    E1_RFP: !v.state.bid,
    E8_SUBMIT_APPROVAL: st === "검토중" || st === "재검토",
    E2_DECISION: st === "상신",
    E7_SUBMIT_PROPOSAL: st === "승인" || st === "조건부 승인",
    E4_BUYER_REPLY: !!v.state.bid,
    E0_CLOCK: true,
    E5_NEW_ORDER: true,
  };
}

// 시나리오에 정해 둔 고정 입력값을 사건 종류별 프리셋으로 쓴다(같은 값은 한 번만).
function presets(v: View, type: string): { label: string; payload: Fields }[] {
  const seen = new Set<string>();
  const out: { label: string; payload: Fields }[] = [];
  for (const s of v.scenarios.flatMap((sc) => sc.steps)) {
    if (s.type !== type || seen.has(s.label)) continue;
    seen.add(s.label);
    out.push({ label: s.label, payload: s.payload as Fields });
  }
  return out;
}

// 사건 입력: 고정 값(프리셋)을 골라 필드에 채우고, 필요하면 필드를 고친 뒤 기록한다.
// only가 있으면 그 사건 종류만 보인다(결정 페이지의 E2 등, 이슈 #53).
export function EventPanel({ v, run, focus, dept, only, title = "사건 입력" }: { v: View; run: Run; focus?: { event: string; n: number } | null; dept?: Department; only?: string[]; title?: string }) {
  // 부서별 권한(이슈 #43): 상태로 가능한 사건 중 내 부서가 넣을 수 있는 것만(시뮬레이션 세션은 대행 포함)
  const all = available(v);
  const state: Record<string, boolean> = only ? Object.fromEntries(Object.entries(all).filter(([k]) => only.includes(k))) : all;
  const perm = (k: string) => canEvent(v, dept, k);
  const avail: Record<string, boolean> = Object.fromEntries(Object.keys(state).map((k) => [k, state[k] && perm(k).ok]));
  // 다음에 할 사건을 먼저 고른다. 내 부서 사건이 대행 사건보다 먼저다(이슈 #47).
  const order = only ? NEXT_ORDER.filter((k) => only.includes(k)) : NEXT_ORDER;
  const first = order.find((k) => avail[k] && !perm(k).proxy) ?? order.find((k) => avail[k]) ?? order.find((k) => perm(k).ok) ?? order[0] ?? "E0_CLOCK";
  const [type, setType] = useState(first);
  const [preset, setPreset] = useState(0);
  const [fields, setFields] = useState<Fields | null>(null);
  const [simple, setSimple] = useState<Fields>({ sim_date: v.state.scenario.sim_date, product_id: "PL-02", qty: 30000, customer_id: "C-102", note: "" });
  useEffect(() => setSimple((s) => ({ ...s, sim_date: v.state.scenario.sim_date })), [v.state.scenario.sim_date]);
  const kind = FORM_KIND[type];
  const choices = kind ? presets(v, type) : [];

  useEffect(() => {
    if (!avail[type]) setType(first);
  }, [v.state.bid?.state]); // eslint-disable-line react-hooks/exhaustive-deps
  // 부서·세션을 바꾸면 그 부서의 다음 사건으로 다시 고른다.
  useEffect(() => setType(first), [dept?.code, v.session.id]); // eslint-disable-line react-hooks/exhaustive-deps

  // 업무 레인 보드에서 고른 사건으로 바꾼다.
  useEffect(() => {
    if (focus && avail[focus.event]) setType(focus.event);
  }, [focus?.n]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    setPreset(0);
    const c = kind ? presets(v, type) : [];
    setFields(c.length ? structuredClone(c[0].payload) : null);
  }, [type]); // eslint-disable-line react-hooks/exhaustive-deps

  const pick = (i: number) => {
    setPreset(i);
    setFields(structuredClone(choices[i].payload));
  };

  const submit = async () => {
    let payload: unknown = {};
    if (kind) payload = fields;
    else if (type === "E0_CLOCK") payload = { sim_date: simple.sim_date };
    else if (type === "E5_NEW_ORDER") payload = { product_id: simple.product_id, qty: Number(simple.qty), customer_id: simple.customer_id };
    else if (type === "E8_SUBMIT_APPROVAL") payload = { note: simple.note };
    await run(() => api.event(type, ACTOR[type], payload));
  };

  return (
    <Section title={title} mode={["real", "mock"]} note="입력은 정리해 둔 고정 값(구조화 필드)만 쓴다. 바이어·이사회 회신은 모의 값이다. 규칙에 맞지 않는 사건은 거부되고 기록되지 않는다. 내 부서 권한 밖의 사건은 막힌다(시뮬레이션 세션은 대행).">
      <label className="field">
        <span>사건</span>
        <select value={type} onChange={(e) => setType(e.target.value)}>
          {Object.entries(EVENT_LABEL)
            .filter(([k]) => k in ACTOR && k in state)
            .map(([k, label]) => {
              const p = perm(k);
              const why = !p.ok ? ` (${p.reason})` : !state[k] ? " (지금 불가)" : p.proxy ? ` (${p.reason})` : "";
              return (
                <option key={k} value={k} disabled={!avail[k]}>
                  {label}{why}
                </option>
              );
            })}
        </select>
      </label>
      {dept && v.session.kind === "main" && (dept.events ?? []).every((e) => e === "E3_REPLY") && (dept.replies ?? []).length > 0 && (
        <p className="small muted">{dept.label} 부서는 확인 요청 회신({(dept.replies ?? []).join("·")})을 넣는다 — 내 업무·확인 요청 페이지</p>
      )}
      {perm(type).proxy && <p className="small warn-text">[모의 대행] {perm(type).reason} — 입력 부서 {dept?.label}로 기록된다</p>}
      {!perm(type).ok && <p className="small bad">{perm(type).reason}</p>}

      {kind && fields && (
        <div className="fieldbox">
          {choices.length > 1 && (
            <label className="field">
              <span>고정 값 선택</span>
              <select value={preset} onChange={(e) => pick(Number(e.target.value))}>
                {choices.map((c, i) => <option key={c.label} value={i}>{c.label}</option>)}
              </select>
            </label>
          )}
          {choices.length === 1 && <p className="muted small">고정 값: {choices[0].label}</p>}
          {kind === "rfp" && <RfpForm f={fields} set={setFields} />}
          {kind === "decision" && <DecisionForm f={fields} set={setFields} codes={v.condition_codes} />}
          {kind === "buyer_reply" && <BuyerForm f={fields} set={setFields} />}
          <button onClick={submit} disabled={!avail[type]}>{EVENT_LABEL[type]} 기록</button>
        </div>
      )}

      {type === "E0_CLOCK" && (
        <label className="field"><span>업무 날짜(앞으로만)</span><input type="date" value={String(simple.sim_date)} onChange={(e) => setSimple({ ...simple, sim_date: e.target.value })} /></label>
      )}
      {type === "E5_NEW_ORDER" && (
        <div className="fieldbox">
          <label className="field"><span>제품군</span><select value={String(simple.product_id)} onChange={(e) => setSimple({ ...simple, product_id: e.target.value })}><option>PL-01</option><option>PL-02</option></select></label>
          <label className="field"><span>수량(EA)</span><input type="number" value={Number(simple.qty)} onChange={(e) => setSimple({ ...simple, qty: e.target.value })} /></label>
          <label className="field"><span>고객</span><input value={String(simple.customer_id)} onChange={(e) => setSimple({ ...simple, customer_id: e.target.value })} /></label>
        </div>
      )}
      {type === "E8_SUBMIT_APPROVAL" && (
        <label className="field"><span>상신 메모</span><input value={String(simple.note)} onChange={(e) => setSimple({ ...simple, note: e.target.value })} placeholder="엔진 제안안 상신" /></label>
      )}
      {!kind && (
        <button onClick={submit} disabled={!avail[type]}>{EVENT_LABEL[type]} 기록</button>
      )}
    </Section>
  );
}

function num(v: string): number | null {
  return v.trim() === "" ? null : Number(v);
}

function RfpForm({ f, set }: { f: Fields; set: (f: Fields) => void }) {
  const items = (f.items as { product_id: string; annual_qty: number; unit_price_usd: number }[]) ?? [];
  const upd = (i: number, k: string, val: string) => {
    const next = items.map((it, j) => (j === i ? { ...it, [k]: k === "product_id" ? val : Number(val) } : it));
    set({ ...f, items: next });
  };
  return (
    <>
      <table className="grid">
        <thead><tr><th>제품군</th><th>연 수량(EA)</th><th>단가(USD)</th></tr></thead>
        <tbody>
          {items.map((it, i) => (
            <tr key={i}>
              <td><input value={it.product_id} onChange={(e) => upd(i, "product_id", e.target.value)} /></td>
              <td><input type="number" value={it.annual_qty} onChange={(e) => upd(i, "annual_qty", e.target.value)} /></td>
              <td><input type="number" step="0.01" value={it.unit_price_usd} onChange={(e) => upd(i, "unit_price_usd", e.target.value)} /></td>
            </tr>
          ))}
        </tbody>
      </table>
      <div className="row">
        <label className="field"><span>통화</span><input value={String(f.currency)} onChange={(e) => set({ ...f, currency: e.target.value })} /></label>
        <label className="field"><span>페널티(월)</span><input type="number" step="0.01" value={Number(f.penalty_rate_monthly)} onChange={(e) => set({ ...f, penalty_rate_monthly: Number(e.target.value) })} /></label>
        <label className="field"><span>마감(주)</span><input type="number" value={Number(f.deadline_weeks)} onChange={(e) => set({ ...f, deadline_weeks: Number(e.target.value) })} /></label>
      </div>
    </>
  );
}

function DecisionForm({ f, set, codes }: { f: Fields; set: (f: Fields) => void; codes: Record<string, string> }) {
  const opts = (f.options as Record<string, string>) ?? {};
  const conds = (f.conditions as string[]) ?? [];
  const toggle = (c: string) => set({ ...f, conditions: conds.includes(c) ? conds.filter((x) => x !== c) : [...conds, c] });
  return (
    <>
      <div className="row">
        <label className="field"><span>참여</span>
          <select value={String(f.participation)} onChange={(e) => set({ ...f, participation: e.target.value })}>{["승인", "조건부", "거절"].map((x) => <option key={x}>{x}</option>)}</select>
        </label>
        {["OPT-02", "OPT-01"].map((o) => (
          <label key={o} className="field"><span>{o}</span>
            <select value={opts[o] ?? "언급없음"} onChange={(e) => set({ ...f, options: { ...opts, [o]: e.target.value } })}>{["승인", "보류", "거절", "언급없음"].map((x) => <option key={x}>{x}</option>)}</select>
          </label>
        ))}
        <label className="field"><span>인하 하한(%)</span><input type="number" value={f.price_floor_pct === null ? "" : Number(f.price_floor_pct)} onChange={(e) => set({ ...f, price_floor_pct: num(e.target.value) })} /></label>
      </div>
      <div className="checks">
        {Object.entries(codes).map(([c, t]) => (
          <label key={c}><input type="checkbox" checked={conds.includes(c)} onChange={() => toggle(c)} /> <code>{c}</code> {t}</label>
        ))}
      </div>
      <label className="field"><span>요지</span><input value={String(f.note ?? "")} onChange={(e) => set({ ...f, note: e.target.value })} /></label>
    </>
  );
}

function tri(v: unknown): string {
  return v === true ? "true" : v === false ? "false" : "";
}
function fromTri(s: string): boolean | null {
  return s === "true" ? true : s === "false" ? false : null;
}

function BuyerForm({ f, set }: { f: Fields; set: (f: Fields) => void }) {
  return (
    <div className="row wrap">
      <label className="field"><span>납품 개시월</span><input placeholder="YYYY-MM" value={String(f.delivery_start ?? "")} onChange={(e) => set({ ...f, delivery_start: e.target.value.trim() || null })} /></label>
      <label className="field"><span>분할 납품</span><select value={tri(f.split_allowed)} onChange={(e) => set({ ...f, split_allowed: fromTri(e.target.value) })}><option value="">언급 없음</option><option value="true">허용</option><option value="false">불가</option></select></label>
      <label className="field"><span>단가 인하(%)</span><input type="number" value={f.price_cut_pct === null ? "" : Number(f.price_cut_pct)} onChange={(e) => set({ ...f, price_cut_pct: num(e.target.value) })} /></label>
      <label className="field"><span>페널티 기준</span><select value={String(f.penalty_basis ?? "")} onChange={(e) => set({ ...f, penalty_basis: e.target.value || null })}><option value="">언급 없음</option><option value="backlog">지연분</option><option value="contract">계약 총액</option></select></label>
      <label className="field"><span>F2 생산지</span><select value={tri(f.f2_acceptance)} onChange={(e) => set({ ...f, f2_acceptance: fromTri(e.target.value) })}><option value="">언급 없음</option><option value="true">수용</option><option value="false">불가</option></select></label>
      <label className="field"><span>첫 달 필수(EA)</span><input type="number" value={f.initial_qty_required === null ? "" : Number(f.initial_qty_required)} onChange={(e) => set({ ...f, initial_qty_required: num(e.target.value) })} /></label>
      <label className="field"><span>계약(개월)</span><input type="number" value={f.contract_months === null ? "" : Number(f.contract_months)} onChange={(e) => set({ ...f, contract_months: num(e.target.value) })} /></label>
    </div>
  );
}

// 확인 요청: 결정 모델이 무엇을 누구에게 물을지 정한다. 내부 부서 회신(E3)은 여기서 모의 입력한다.
export function RequestsView({ v, run, dept }: { v: View; run: Run; dept?: Department }) {
  const rs = v.state.requests ?? [];
  const [open, setOpen] = useState<string | null>(null);
  return (
    <Section title={`확인 요청 (열림 ${rs.filter((r) => r.state !== "반영").length})`} mode={["real", "mock"]} note="요청 생성·기한 초과·재요청은 실제 작동, 회신 내용은 모의다. 바이어 항목(CR-04~06)은 E4 바이어 회신으로 반영된다. 회신은 요청 대상 부서만 넣는다(시뮬레이션 세션은 대행).">
      {rs.length === 0 && <p className="muted small">RFP 접수(E1) 뒤 생성된다.</p>}
      <table className="grid text">
        <thead><tr><th>ID</th><th>대상</th><th>질문</th><th>막는 판단</th><th>기한</th><th>상태</th><th /></tr></thead>
        <tbody>
          {rs.map((r) => {
            const p = canEvent(v, dept, "E3_REPLY", r);
            return (
              <tr key={r.request_id} className={dept && r.to_role === dept.id ? "row-strong" : ""}>
                <td>{r.request_id}</td><td>{r.to_role}</td><td>{r.question}</td><td className="small">{(r.blocks ?? []).join(", ")}</td><td>{r.due_date}</td>
                <td><span className={`pill pill-${slug(r.state)}`}>{r.state}</span>{r.mock_reply && <span className="muted small"> 모의</span>}</td>
                <td>
                  {r.state !== "반영" && r.to_role !== "바이어" && (
                    <button className="ghost small" disabled={!p.ok} title={p.reason ?? ""} onClick={() => setOpen(open === r.request_id ? null : r.request_id)}>
                      {p.proxy ? "대행 회신" : "회신 입력"}
                    </button>
                  )}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
      {open && <ReplyForm r={rs.find((x) => x.request_id === open)!} run={run} done={() => setOpen(null)} />}
    </Section>
  );
}

// CR-10: PL-01을 옮겨 만들 후보 라인(PL-01 자기 라인 L-01과 더마 전용 L-02 제외, 서버 otherLinesForBasic과 같다)
const OTHER_LINES = ["L-03", "L-11", "L-12", "L-13"];

function ReplyForm({ r, run, done }: { r: ConfirmRequest; run: Run; done: () => void }) {
  const [a, setA] = useState<Fields>({ capacity_period: "annual", extra_annual: 0, means: "", masil_in_util: false, mb_ready: true, validation_weeks: 8, released_qty: 0, revised: false, release_pl01: 0, release_pl02: 0, lines: [] as string[], changeover_weeks: 4, annual_qty: 0, received: false, orders: 0 });
  const answer = (): Fields => {
    switch (r.request_id) {
      case "CR-01": return { capacity_period: a.capacity_period };
      case "CR-02": return { extra_annual: Number(a.extra_annual), means: a.means };
      case "CR-03": return { masil_in_util: a.masil_in_util };
      case "CR-07": return { mb_ready: a.mb_ready, validation_weeks: Number(a.validation_weeks) };
      case "CR-08": return { released_qty: Number(a.released_qty) };
      case "CR-09": return { revised: a.revised, release_pl01: a.revised ? Number(a.release_pl01) : 0, release_pl02: a.revised ? Number(a.release_pl02) : 0 };
      case "CR-10": return { lines: a.lines, changeover_weeks: Number(a.changeover_weeks), annual_qty: Number(a.annual_qty) };
      case "CR-11": return { received: a.received, orders: Number(a.orders) };
    }
    return {};
  };
  const send = async () => {
    await run(() => api.event("E3_REPLY", r.to_role, { request_id: r.request_id, answer: answer() }));
    done();
  };
  return (
    <div className="fieldbox">
      <p className="small"><b>{r.request_id}</b> {r.to_role} 회신 [모의] — {r.question}</p>
      <div className="row wrap">
        {r.request_id === "CR-01" && (
          <label className="field"><span>기간 단위</span><select value={String(a.capacity_period)} onChange={(e) => setA({ ...a, capacity_period: e.target.value })}><option value="annual">연간</option><option value="monthly">월간</option></select></label>
        )}
        {r.request_id === "CR-02" && (
          <>
            <label className="field"><span>연간 추가 가능(EA)</span><input type="number" value={Number(a.extra_annual)} onChange={(e) => setA({ ...a, extra_annual: e.target.value })} /></label>
            <label className="field"><span>수단</span><input value={String(a.means)} onChange={(e) => setA({ ...a, means: e.target.value })} placeholder="없음 / 외주 / 잔업" /></label>
          </>
        )}
        {r.request_id === "CR-03" && (
          <label className="field"><span>Masil 발주가 계획에 포함</span><select value={String(a.masil_in_util)} onChange={(e) => setA({ ...a, masil_in_util: e.target.value === "true" })}><option value="false">아니오</option><option value="true">예</option></select></label>
        )}
        {r.request_id === "CR-07" && (
          <>
            <label className="field"><span>M-B 재계약</span><select value={String(a.mb_ready)} onChange={(e) => setA({ ...a, mb_ready: e.target.value === "true" })}><option value="true">가능</option><option value="false">불가</option></select></label>
            <label className="field"><span>재검증(주)</span><input type="number" value={Number(a.validation_weeks)} onChange={(e) => setA({ ...a, validation_weeks: e.target.value })} /></label>
          </>
        )}
        {r.request_id === "CR-08" && (
          <label className="field"><span>늦출 수 있는 수량(EA, 최대 {(r.max_qty ?? 0).toLocaleString("ko-KR")} = 업무일 Masil 보호량)</span><input type="number" min={0} max={r.max_qty ?? 0} value={Number(a.released_qty)} onChange={(e) => setA({ ...a, released_qty: e.target.value })} /></label>
        )}
        {r.request_id === "CR-09" && (
          <>
            <label className="field"><span>계약·예측 수정 확인</span><select value={String(a.revised)} onChange={(e) => setA({ ...a, revised: e.target.value === "true" })}><option value="false">아니오(반환 0)</option><option value="true">예</option></select></label>
            <label className="field"><span>PL-01 반환(연 EA)</span><input type="number" min={0} disabled={!a.revised} value={Number(a.release_pl01)} onChange={(e) => setA({ ...a, release_pl01: e.target.value })} /></label>
            <label className="field"><span>PL-02 반환(연 EA)</span><input type="number" min={0} disabled={!a.revised} value={Number(a.release_pl02)} onChange={(e) => setA({ ...a, release_pl02: e.target.value })} /></label>
          </>
        )}
        {r.request_id === "CR-10" && (
          <>
            <span className="field">
              <span>옮겨 만들 라인</span>
              <span className="row wrap">
                {OTHER_LINES.map((id) => {
                  const sel = (a.lines as string[]).includes(id);
                  return (
                    <label key={id} className="small">
                      <input type="checkbox" checked={sel} onChange={() => setA({ ...a, lines: sel ? (a.lines as string[]).filter((x) => x !== id) : [...(a.lines as string[]), id] })} /> {id}
                    </label>
                  );
                })}
              </span>
            </span>
            <label className="field"><span>전환·셋업(주)</span><input type="number" min={0} value={Number(a.changeover_weeks)} onChange={(e) => setA({ ...a, changeover_weeks: e.target.value })} /></label>
            <label className="field"><span>연간 가능 수량(EA)</span><input type="number" min={0} value={Number(a.annual_qty)} onChange={(e) => setA({ ...a, annual_qty: e.target.value })} /></label>
          </>
        )}
        {r.request_id === "CR-11" && (
          <>
            <label className="field"><span>확정 납기 대응표</span><select value={String(a.received)} onChange={(e) => setA({ ...a, received: e.target.value === "true" })}><option value="false">없음</option><option value="true">받음</option></select></label>
            <label className="field"><span>대응표 주문 수(건)</span><input type="number" min={0} value={Number(a.orders)} onChange={(e) => setA({ ...a, orders: e.target.value })} /></label>
          </>
        )}
      </div>
      {r.request_id === "CR-09" && <p className="muted small">회신 권한: 영업 임원(기존 고객 약속 조정). 다른 역할이 넣으면 거절된다.</p>}
      {(r.request_id === "CR-10" || r.request_id === "CR-11") && <p className="muted small">회신은 기록하고 후속 작업을 만든다. 엔진 계산 반영(타 라인 공급, 추정 납기 대체)은 설계 단계다.</p>}
      <button onClick={send}>회신 기록(E3)</button>
    </div>
  );
}
