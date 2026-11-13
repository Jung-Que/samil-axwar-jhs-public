import type { Department, LaneAction, View } from "./types";
import { Kpi, Mode, Section } from "./panels";
import { EVENT_LABEL, ea, eok, pct } from "./format";
import { myWork } from "./perm";
import type { PageId as Tab } from "./frame";
import { EmptyState } from "./brand";
import { Link } from "./router";

const FEATURE_LABEL: Record<string, string> = {
  view: "조회",
  "sim.session": "시뮬레이션 세션 생성·분기·시연",
  "sim.act": "시뮬레이션 대행 입력·모의 가정",
  sweep: "민감도 격자·세션 비교",
  "main.clock": "기준 세션 시계",
  "main.reset": "기준 세션 초기화·시연",
  "session.delete": "시뮬레이션 세션 삭제",
};

// ---- 입찰 흐름(이슈 #47): 사건 → 판단 → 사람 승인 → 작업·상태. 단계는 입찰 상태(서버 state.go Bid*)에서 읽는다. ----

const FLOW = [
  { type: "E1_RFP", label: "RFP 접수" },
  { type: "E8_SUBMIT_APPROVAL", label: "상신" },
  { type: "E2_DECISION", label: "승인 결정" },
  { type: "E7_SUBMIT_PROPOSAL", label: "제안 제출" },
  { type: "E4_BUYER_REPLY", label: "바이어 회신" },
];

type StepState = "done" | "now" | "todo" | "stop";
type FlowStep = { type: string; label: string; state: StepState; who?: Department; note: string };

function flowSteps(v: View): { steps: FlowStep[]; now: number } {
  const st = v.state.bid?.state;
  // 지금 단계. 보류·수주·실주는 진행 중인 단계가 없다(-1).
  const now = !st ? 0 : st === "검토중" || st === "재검토" ? 1 : st === "상신" ? 2 : st === "승인" || st === "조건부 승인" ? 3 : st === "회신 대기" ? 4 : -1;
  const doneUpTo = now >= 0 ? now : st === "보류" ? 2 : FLOW.length;
  const log = v.state.log ?? [];
  const steps = FLOW.map((f, i): FlowStep => {
    const who = (v.departments ?? []).find((d) => (d.events ?? []).includes(f.type));
    const e = [...log].reverse().find((x) => x.event.type === f.type)?.event;
    let state: StepState = i < doneUpTo ? "done" : i === now ? "now" : "todo";
    let note = who ? who.label : "";
    if (state === "done" && e) note = `${e.by && e.by !== e.actor_role ? `${e.actor_role}(대행 ${e.by})` : e.actor_role} · ${e.sim_date.slice(5)}`;
    if (state === "now") note = `현재 · ${who?.label ?? ""}`;
    if (st === "보류" && i === 2) {
      state = "stop";
      note = "거절 → 입찰 보류";
    }
    if (st === "재검토" && i === 1) note = `재상신 · ${who?.label ?? ""}(승인 후 조건 변경)`;
    if ((st === "수주" || st === "실주") && i === 4) note = `종결: ${st}`;
    return { ...f, state, who, note };
  });
  return { steps, now };
}

function BidFlow({ v, dept, onChoose }: { v: View; dept?: Department; onChoose: (code: string) => void }) {
  const { steps, now } = flowSteps(v);
  const cur = now >= 0 ? steps[now] : undefined;
  const turn = cur?.who && cur.who.code !== dept?.code ? cur.who : undefined;
  return (
    <Section title="입찰 흐름" mode={["real"]} note="단계는 입찰 상태를 기준으로 표시합니다. 각 단계는 해당 부서 권한으로 입력합니다(시뮬레이션 세션에서는 대행 입력을 기록하며 허용).">
      <ol className="flow">
        {steps.map((s, i) => (
          <li key={s.type} className={`flow-step is-${s.state}`} aria-current={s.state === "now" ? "step" : undefined}>
            <span className="flow-title">
              {i + 1} {s.label}
            </span>
            <span className="flow-sub">{s.note}</span>
          </li>
        ))}
      </ol>
      {turn && (
        <p className="flow-next small">
          다음은 <b>{turn.label}</b> 차례입니다. {v.session.kind === "sim" ? "시뮬레이션 세션에서는 현재 부서로 대행 입력할 수 있습니다." : "기준 세션에서는 해당 부서만 입력할 수 있습니다."}{" "}
          <button className="ghost small" onClick={() => onChoose(turn.code)}>{turn.label} 계정으로 다시 로그인</button>
        </p>
      )}
    </Section>
  );
}

// ---- 지금 할 일: 내 부서 권한으로 지금 넣을 수 있는 사건 중 첫 번째와 그 이유 ----

function why(a: LaneAction, v: View): string {
  const conds = [...(v.state.decisions ?? [])].reverse().find((d) => d.freshness === "최신")?.conditions?.length ?? 0;
  switch (a.event) {
    case "E1_RFP":
      return "RFP를 접수하면 엔진이 원본 데이터로 판단하고, 확인이 필요한 항목은 각 부서에 확인 요청으로 전달됩니다.";
    case "E8_SUBMIT_APPROVAL":
      return `판단 카드(평가 v${v.state.eval_version})를 CEO/이사회에 상신합니다. 승인 권한은 입찰 참여·투자 결정권자에게 있습니다.`;
    case "E2_DECISION":
      return "상신된 제안을 승인·조건부 승인·거절 중 하나로 결정합니다. 결정은 당시 제안 내용을 기준으로 하며, 내용이 바뀌면 낡음으로 표시됩니다.";
    case "E7_SUBMIT_PROPOSAL":
      return `승인 조건 ${conds}개를 반영한 제안을 바이어에게 제출합니다. 제출하면 C-201 가예약이 생성되며, 기존 확정 약속은 미루지 않습니다.`;
    case "E4_BUYER_REPLY":
      return "바이어 회신(개시일·분할·페널티·생산지)을 입력하면 엔진이 다시 계산합니다. 승인 내용과 달라지면 재상신이 필요합니다.";
    case "E3_REPLY":
      return `확인 요청 ${a.request_id ?? ""}에 회신하면 그 값으로 다시 계산되고, 막혀 있던 항목이 해제됩니다.`;
    case "E5_NEW_ORDER":
      return "신규 수주를 입력하면 약속 원장과 라인 여유가 다시 계산됩니다.";
    default:
      return EVENT_LABEL[a.event] ?? a.event;
  }
}

function NowCard({ v, dept, actions, onAction }: { v: View; dept: Department; actions: LaneAction[]; onAction: (a: LaneAction) => void }) {
  const enabled = actions.filter((a) => a.enabled);
  const first = enabled[0];
  const blocked = actions.filter((a) => !a.enabled && a.reason);
  const dec = [...(v.state.decisions ?? [])].reverse().find((d) => d.freshness === "최신");
  const preSubmit = v.state.bid?.state === "승인" || v.state.bid?.state === "조건부 승인";
  const conds = preSubmit && (dept.events ?? []).includes("E7_SUBMIT_PROPOSAL") ? (dec?.conditions ?? []) : [];
  const blocks = v.state.last_blocks ?? [];
  const { steps, now } = flowSteps(v);
  const cur = now >= 0 ? steps[now] : undefined;
  return (
    <section className={`card now-card ${first ? "has-action" : ""}`}>
      <header className="card-head">
        <h2>지금 할 일</h2>
        <span className="modes"><Mode kind="real" /></span>
      </header>
      {first ? (
        <div className="now-main">
          <div className="now-text">
            <p className="now-title">{first.label}</p>
            <p className="muted small">{why(first, v)}</p>
          </div>
          <div className="row">
            {enabled.slice(1, 4).map((a) => (
              <button key={a.label} className="ghost" onClick={() => onAction(a)}>{a.label}</button>
            ))}
            <button onClick={() => onAction(first)}>{first.label}</button>
          </div>
        </div>
      ) : (
        <EmptyState compact swan="done" title="현재 부서에서 처리할 업무가 없습니다" action={<Link to="/flow" className="btn ghost-btn small-btn">시연 흐름 보기</Link>}>
          {cur?.who ? `현재 입찰은 '${cur.label}' 단계이며 ${cur.who.label} 차례입니다. 소속 부서 차례가 오면 여기에 표시됩니다.` : v.state.bid ? `입찰 상태: ${v.state.bid.state}.` : "아직 접수된 입찰이 없습니다."}
          {blocked[0] ? ` 막힌 일: ${blocked[0].label} — ${blocked[0].reason}` : ""}
        </EmptyState>
      )}
      {dec && conds.length > 0 && (
        <>
          <h3>
            승인 조건 {dec.decision_id} ({conds.length}) <span className="muted small">· 제출 전 점검표 · {dec.approver_role} · {dec.decided_at}</span>
          </h3>
          <ul className="conds now-conds">
            {conds.map((c) => (
              <li key={c}>
                <span className="rule">{c}</span> {v.condition_codes[c] ?? ""}
              </li>
            ))}
          </ul>
        </>
      )}
      {blocks.length > 0 && <p className="blocked">제출 차단: {blocks.join(" / ")}</p>}
    </section>
  );
}

// ---- 판단 요약과 다른 부서가 답할 것 ----

function JudgeSummary({ v, goTab }: { v: View; goTab: (t: Tab) => void }) {
  const ev = v.state.evaluation;
  const p1 = ev.products["PL-01"];
  const p2 = ev.products["PL-02"];
  const l1 = v.state.lines?.find((l) => l.product_id === "PL-01");
  return (
    <Section title="판단 요약" mode={["real"]} note="엔진 계산값입니다(능력 스냅샷 기준). 근거와 플래그는 C-201 판단·제안 페이지에서 확인할 수 있습니다.">
      <div className="kpis one-col">
        <Kpi label="PL-02 더마 제안 가능" value={`${ea(p2?.schedule_total)} EA`} sub={`요구 ${ea((p2?.monthly_demand ?? 0) * ev.scenario.contract_months)} 중 ${pct(p2?.coverage_pct)}`} tone={p2 && p2.coverage_pct < 100 ? "warn" : "ok"} />
        <Kpi label="PL-01 기초" value={l1?.qty_status === "미확정" || !l1 ? "수량 미확정" : `${ea(p1?.schedule_total)} EA`} sub={`확인 전 상한 월 ${ea(p1?.months?.[0]?.supply)} EA`} tone="bad" />
        <Kpi label="전량 약속 시 지체상금(지연분)" value={eok((p1?.penalty_backlog_krw ?? 0) + (p2?.penalty_backlog_krw ?? 0))} sub={`계약 총액 기준 ${eok(ev.penalty_contract_krw)}`} tone="bad" />
      </div>
      <button className="ghost small" onClick={() => goTab("judge")}>판단·제안 보기</button>
    </Section>
  );
}

function OthersOwe({ v, dept }: { v: View; dept?: Department }) {
  const open = (v.state.requests ?? []).filter((r) => r.state !== "반영" && r.to_role !== dept?.id);
  const byRole = new Map<string, string[]>();
  for (const r of open) byRole.set(r.to_role, [...(byRole.get(r.to_role) ?? []), `${r.request_id}${r.state === "기한 초과" ? "(기한 초과)" : ""}`]);
  return (
    <Section title={`다른 부서가 답할 것 (${open.length})`} mode={["real"]} note="열린 확인 요청을 받는 부서별로 묶어 표시합니다(회신 값은 모의).">
      {open.length === 0 ? (
        <p className="muted small">현재 다른 부서에 열린 확인 요청이 없습니다.</p>
      ) : (
        <ul className="tasks">
          {[...byRole].map(([role, ids]) => (
            <li key={role}>
              <b>{role}</b> <span className="small">{ids.join(" · ")}</span>
            </li>
          ))}
        </ul>
      )}
    </Section>
  );
}

// ---- 내 업무(이슈 #43 → #47): 지금 할 일·입찰 흐름·나에게 온 확인 요청·내 작업·다른 부서·판단 요약·권한표 ----

type MyWorkProps = {
  v: View;
  dept?: Department;
  onAction: (a: LaneAction) => void;
  onChoose: (code: string) => void;
  onFocus: (event: string) => void;
  goTab: (t: Tab) => void;
};

export function MyWorkView({ v, dept, onAction, onChoose, onFocus, goTab }: MyWorkProps) {
  if (!dept) {
    return (
      <div className="split">
        <div className="split-main">
          <Section title="내 업무" mode={["real"]} note="로그인한 계정의 역할을 기준으로 현재 할 일과 권한을 표시합니다. 시연할 때는 입찰 흐름 순서에 따라 해당 부서 계정으로 다시 로그인해 입력합니다.">
            <p className="small">로그인한 계정의 역할을 권한표에서 찾을 수 없습니다. 아래 입찰 흐름에서 현재 차례인 부서 계정으로 다시 로그인해 주세요.</p>
          </Section>
          <BidFlow v={v} onChoose={onChoose} />
          <PermissionSection v={v} open />
        </div>
      </div>
    );
  }
  const { reqs, tasks, actions } = myWork(v, dept);
  const buyer = (v.state.requests ?? []).filter((r) => r.to_role === "바이어" && r.state !== "반영");
  const handlesBuyer = (dept.events ?? []).includes("E4_BUYER_REPLY");
  const mine = [...(v.state.log ?? [])].reverse().filter((e) => e.event.actor_role === dept.id || e.event.by === dept.id).slice(0, 8);
  const sim = v.session.kind === "sim";
  return (
    <div className="split">
      <div className="split-main">
        <NowCard v={v} dept={dept} actions={actions} onAction={onAction} />
        <BidFlow v={v} dept={dept} onChoose={onChoose} />
        <div className="cols">
          <Section title={`나에게 온 확인 요청 (${reqs.length})`} mode={["real", "mock"]} note={sim ? "시뮬레이션 세션에서는 다른 부서 요청도 확인 요청 페이지에서 대행 회신할 수 있습니다." : "기준 세션에서는 소속 부서로 온 요청에만 회신할 수 있습니다."}>
            {reqs.length === 0 ? (
              <p className="muted small">현재 받은 확인 요청이 없습니다. 요청이 오면 여기에 표시됩니다.</p>
            ) : (
              <ul className="tasks">
                {reqs.map((r) => (
                  <li key={r.request_id}>
                    <span className="role">{r.request_id}</span> {r.question}{" "}
                    <span className="muted small">
                      기한 {r.due_date} · <span className={r.state === "기한 초과" ? "bad" : ""}>{r.state}</span>
                    </span>{" "}
                    <button className="ghost small" onClick={() => goTab("requests")}>회신 입력</button>
                  </li>
                ))}
              </ul>
            )}
            {handlesBuyer && buyer.length > 0 && (
              <>
                <h3>바이어 회신 대기 ({buyer.length})</h3>
                <p className="small">
                  {buyer.map((r) => (
                    <span key={r.request_id}>
                      <span className="role">{r.request_id}</span>
                      {r.question.split("(")[0]}{" "}
                    </span>
                  ))}
                </p>
                <div className="row">
                  <button className="ghost small" onClick={() => onFocus("E4_BUYER_REPLY")}>E4 바이어 회신 입력</button>
                  <span className="muted small">기한 {buyer[0].due_date} · 영업이 받은 회신을 입력합니다(값은 모의)</span>
                </div>
              </>
            )}
          </Section>
          <Section title={`내 작업 (${tasks.length})`} mode={["real"]} note="사건으로 생성된 작업이며, 사건 번호로 원인을 추적할 수 있습니다.">
            {tasks.length === 0 ? (
              <p className="muted small">현재 열린 작업이 없습니다. 사건으로 작업이 생성되면 여기에 표시됩니다.</p>
            ) : (
              <ul className="tasks">
                {tasks.map((t) => (
                  <li key={t.task_id}>
                    {t.title} <span className="muted small">{t.due_date ? `~${t.due_date}` : ""} · 사건 #{t.cause_seq}</span>
                  </li>
                ))}
              </ul>
            )}
            <h3>내 부서 입력 이력 ({mine.length})</h3>
            {mine.length === 0 ? (
              <p className="muted small">현재 입력 이력이 없습니다.</p>
            ) : (
              <ul className="tasks">
                {mine.map((e) => (
                  <li key={e.event.seq}>
                    #{e.event.seq} {EVENT_LABEL[e.event.type] ?? e.event.type}{" "}
                    <span className="muted small">
                      권한 {e.event.actor_role}
                      {e.event.by && e.event.by !== e.event.actor_role ? ` · 대행 입력 ${e.event.by}` : ""} · 업무일 {e.event.sim_date}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </Section>
        </div>
      </div>
      <div className="split-side">
        <JudgeSummary v={v} goTab={goTab} />
        <OthersOwe v={v} dept={dept} />
        <PermissionSection v={v} current={dept.code} />
      </div>
    </div>
  );
}

function PermissionSection({ v, current, open }: { v: View; current?: string; open?: boolean }) {
  return (
    <Section title="부서별 권한표" mode={["real"]} note="서버는 이 권한표로 판단합니다. 관리 › 사용자·권한에서도 확인할 수 있습니다.">
      <details open={open}>
        <summary className="small">표 펼치기</summary>
        <PermissionTable v={v} current={current} />
      </details>
    </Section>
  );
}

export function PermissionTable({ v, current }: { v: View; current?: string }) {
  return (
    <div className="scroll">
      <table className="grid text">
        <thead>
          <tr>
            <th>권한</th>
            <th>기준 세션 입력 사건</th>
            <th>회신</th>
            <th>기능</th>
          </tr>
        </thead>
        <tbody>
          {(v.departments ?? []).map((d) => (
            <tr key={d.code} className={d.code === current ? "row-strong" : ""}>
              <td className="nowrap">{d.dept === d.label ? d.label : `${d.dept} · ${d.label}`}</td>
              <td className="small">{(d.events ?? []).filter((e) => e !== "E3_REPLY").map((e) => EVENT_LABEL[e] ?? e).join(" · ") || "-"}</td>
              <td className="small">{(d.replies ?? []).join(" · ") || "-"}</td>
              <td className="small">{d.features.map((f) => FEATURE_LABEL[f] ?? f).join(" · ")}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
