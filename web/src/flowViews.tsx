import { useEffect, useState } from "react";
import { auth } from "./api";
import type { Account, Department, User, View } from "./types";
import { Section } from "./panels";
import { pct } from "./format";
import { Link } from "./router";
import { EmptyState } from "./brand";

// 시연 흐름(이슈 #57): 로그인부터 부서 계정을 바꿔 가며 C-201 한 건을 끝까지 따라간다.
// 사건 입력 → 데이터 확인 → 판단(엔진) → 사람 승인 → 작업·상태 갱신 → 다음 사건이 그 상태를 읽음(A01 이벤트 루프).
// 단계 상태는 지금 세션의 입찰 상태·사건 로그·확인 요청·작업에서 읽는다(화면이 따로 기억하지 않는다).

// A01 이벤트 루프 여섯 칸
export const LOOP = ["사건 입력", "데이터 확인", "판단", "사람 승인", "작업·상태 갱신", "다음 사건이 상태를 읽음"];

export type FlowStatus = "done" | "now" | "open" | "skip" | "wait";

export type FlowStep = {
  n: number;
  roles: string[]; // 역할 코드(시스템이면 빈 배열)
  title: string;
  desc: string;
  stage: number; // 이벤트 루프의 어느 칸인지(LOOP 번호)
  to: string; // 갈 페이지 주소
  optional?: boolean;
  status: FlowStatus;
  evidence?: string;
};

// 입찰 상태의 진행 순위: 상태가 이 순위 이상이면 그 단계까지 끝났다.
const RANK: Record<string, number> = { 검토중: 1, 재검토: 1, 상신: 2, 승인: 3, "조건부 승인": 3, 보류: 3, "회신 대기": 4, 수주: 5, 실주: 5 };

// 확인 요청을 받는 부서 ID → 역할 코드(권한표의 ID와 코드)
function codeOf(v: View, deptId: string): string | undefined {
  return (v.departments ?? []).find((d) => d.id === deptId)?.code;
}

export function flowOf(v: View): { steps: FlowStep[]; round: number; doneCount: number; now?: FlowStep } {
  const log = v.state.log ?? [];
  const bid = v.state.bid;
  const rank = bid ? RANK[bid.state] ?? 0 : 0;
  const last = (type: string) => [...log].reverse().find((e) => e.event.type === type)?.event;
  const ev = (type: string) => {
    const e = last(type);
    return e ? `사건 #${e.seq} · 업무일 ${e.sim_date}${e.by && e.by !== e.actor_role ? ` · 대행 ${e.by}` : ""}` : undefined;
  };
  const reqs = (v.state.requests ?? []).filter((r) => r.to_role !== "바이어");
  const prodReqs = reqs.filter((r) => r.to_role === "생산관리");
  const otherReqs = reqs.filter((r) => r.to_role !== "생산관리");
  const open = (rs: typeof reqs) => rs.filter((r) => r.state !== "반영");
  const otherRoles = [...new Set(open(otherReqs).map((r) => codeOf(v, r.to_role)).filter((c): c is string => !!c))];
  const submit = last("E7_SUBMIT_PROPOSAL");
  // 이번 회차의 결정(E2)·제출(E7)이 만든 작업
  const decision = last("E2_DECISION");
  const tasksFromSubmit = submit && decision ? (v.state.tasks ?? []).filter((t) => t.cause_seq >= Math.min(decision.seq, submit.seq)) : [];
  const taskRoles = [...new Set(tasksFromSubmit.map((t) => codeOf(v, t.owner_role === "그룹 재무" ? "재무·전략" : t.owner_role)).filter((c): c is string => !!c))]
    .sort((a, b) => Number(b === "production") - Number(a === "production"));
  const buyerAfterSubmit = !!submit && log.some((e) => e.event.type === "E4_BUYER_REPLY" && e.event.seq > submit.seq);
  const p2 = v.state.evaluation.products["PL-02"];
  const round = log.filter((e) => e.event.type === "E8_SUBMIT_APPROVAL").length;

  const raw: Omit<FlowStep, "status">[] = [
    { n: 1, roles: ["sales"], title: "RFP 접수(E1)", stage: 0, to: "/events/new?type=E1_RFP",
      desc: "영업이 C-201 RFP 원본 값(rfp_c201)을 입력합니다." },
    { n: 2, roles: [], title: "엔진 판단·확인 요청 생성", stage: 2, to: "/bids/C-201",
      desc: "엔진이 원본 데이터로 여유·공급·지체상금을 계산하고, 확인이 필요한 항목은 부서에 확인 요청으로 보냅니다." },
    { n: 3, roles: ["production"], title: "생산관리 회신(E3)", stage: 1, to: "/requests", optional: true,
      desc: "능력 단위·PL-01 증산 수단·Masil 포함 여부를 회신합니다. 건너뛰면 미확정으로 판단합니다(정보 부족 경로)." },
    { n: 4, roles: otherRoles, title: "다른 부서 회신(E3)", stage: 1, to: "/requests", optional: true,
      desc: "구매·품질·Masil SCM·영업 임원에게 보낸 확인 요청이 있으면 회신합니다." },
    { n: 5, roles: ["sales"], title: round > 1 || bid?.state === "재검토" ? "바뀐 조건으로 재상신(E8)" : "엔진 제안안 상신(E8)", stage: 3, to: "/events/new?type=E8_SUBMIT_APPROVAL",
      desc: "영업이 엔진 제안안을 CEO/이사회에 상신합니다." },
    { n: 6, roles: ["board"], title: "참여·대안 결정(E2)", stage: 3, to: "/approvals",
      desc: "CEO/이사회가 참여 여부와 OPT-01·OPT-02를 결정합니다(승인권한 규정)." },
    { n: 7, roles: ["sales"], title: "제안 제출(E7)", stage: 4, to: "/events/new?type=E7_SUBMIT_PROPOSAL",
      desc: "점검표를 통과하면 제출하고, 가예약·생산계획 작업이 생성됩니다." },
    { n: 8, roles: taskRoles.length ? taskRoles : ["production"], title: "생성된 작업 확인", stage: 4, to: "/tasks",
      desc: "결정·제출로 생성된 작업(원인 사건 번호)을 담당 부서가 내 업무·작업에서 확인합니다." },
    { n: 9, roles: ["sales"], title: "바이어 회신(E4)", stage: 5, to: "/events/new?type=E4_BUYER_REPLY",
      desc: "변경된 조건이 들어오면 엔진이 현재 상태로 다시 판단합니다. 결정이 낡으면 5번(재상신)부터 다시 진행합니다." },
  ];

  const done: Record<number, boolean> = {
    1: !!bid,
    2: !!bid,
    3: !!bid && prodReqs.length > 0 && open(prodReqs).length === 0,
    4: !!bid && open(otherReqs).length === 0,
    5: rank >= 2,
    6: rank >= 3,
    7: rank >= 4,
    8: false, // 확인하는 사건이 없어 '완료'로 두지 않는다. 지나가면 '확인 안 함'(skip)
    9: rank >= 5 || buyerAfterSubmit,
  };
  const evidence: Record<number, string | undefined> = {
    1: ev("E1_RFP"),
    2: bid ? `확인 요청 ${(v.state.requests ?? []).length}건(부서 ${reqs.length} · 바이어 ${(v.state.requests ?? []).length - reqs.length}) · PL-02 공급률 ${pct(p2?.coverage_pct)}` : undefined,
    3: bid ? `반영 ${prodReqs.length - open(prodReqs).length}/${prodReqs.length}${open(prodReqs).length ? ` · 열림 ${open(prodReqs).map((r) => r.request_id).join("·")}` : ""}` : undefined,
    4: bid ? (otherReqs.length ? `반영 ${otherReqs.length - open(otherReqs).length}/${otherReqs.length}${open(otherReqs).length ? ` · 열림 ${open(otherReqs).map((r) => `${r.request_id}(${r.to_role})`).join(" ")}` : ""}` : "다른 부서 요청 없음") : undefined,
    5: rank >= 2 ? ev("E8_SUBMIT_APPROVAL") : round > 0 ? `${round}회 상신 · 현재 ${bid?.state}` : undefined,
    6: rank >= 3 ? `${ev("E2_DECISION") ?? ""} · ${bid?.state}` : undefined,
    7: rank >= 4 ? ev("E7_SUBMIT_PROPOSAL") : undefined,
    8: rank >= 4 ? `작업 ${tasksFromSubmit.length}건: ${tasksFromSubmit.map((t) => `${t.owner_role} ${t.title}`).join(" · ") || "없음"}` : undefined,
    9: buyerAfterSubmit ? `${ev("E4_BUYER_REPLY")} · 현재 ${bid?.state}` : undefined,
  };

  // 지금 단계 = 끝나지 않은 첫 필수 단계. 그 앞의 끝나지 않은 선택 단계는 "할 수 있음"(상신 전이면) 또는 "건너뜀".
  // 시연 순서대로 생산관리 회신(3)이 열려 있으면 그것을 지금 단계로 권하고, 상신(5)은 "할 수 있음"(건너뛰고 진행)으로 둔다.
  // 생긴 작업 확인(8)은 사건이 없는 확인 단계라 제출 뒤 다음 사건(9) 전까지 "할 수 있음"이다.
  const firstReq = raw.find((s) => !s.optional && s.n !== 8 && !done[s.n]);
  const prodFirst = !!bid && rank < 2 && !done[3] && prodReqs.length > 0;
  const steps: FlowStep[] = raw.map((s) => {
    let status: FlowStatus;
    if (done[s.n]) status = "done";
    else if (s.n === 8) status = rank >= 5 || buyerAfterSubmit ? "skip" : rank >= 4 ? "open" : "wait";
    else if (s.n === 3 && prodFirst) status = "now";
    else if (s.optional) status = !bid ? "wait" : firstReq && firstReq.n > s.n && rank < 2 ? "open" : "skip";
    else if (s === firstReq) status = prodFirst ? "open" : "now";
    else status = "wait";
    return { ...s, status, evidence: evidence[s.n] };
  });
  // 결정이 보류면 흐름이 멈춘다(재상신 규칙은 엔진 몫)
  if (bid?.state === "보류") steps.forEach((s) => s.n === 7 && (s.desc = "결정이 보류되어 제출할 수 없습니다. 조건을 바꿔 재상신하거나 시뮬레이션에서 먼저 확인해 주세요."));
  return { steps, round, doneCount: steps.filter((s) => s.status === "done").length, now: steps.find((s) => s.status === "now") };
}

const STATUS_LABEL: Record<FlowStatus, string> = { done: "완료", now: "지금", open: "할 수 있음(선택)", skip: "건너뜀", wait: "대기" };

type Props = { v: View; user: User; dept?: Department; onSwitch: (role: string, next: string) => void };

// 역할 코드 → 시연 계정(로그인 화면과 같은 목록). 공개 API라 한 번만 받는다.
let accountsCache: Account[] | null = null;
function useAccounts(): Account[] {
  const [list, setList] = useState<Account[]>(accountsCache ?? []);
  useEffect(() => {
    if (accountsCache) return;
    auth.accounts().then((r) => {
      accountsCache = r.accounts;
      setList(r.accounts);
    }).catch(() => undefined);
  }, []);
  return list;
}

function StepAction({ s, user, accounts, onSwitch }: { s: FlowStep; user: User; accounts: Account[]; onSwitch: Props["onSwitch"] }) {
  if (s.status !== "now" && s.status !== "open") return null;
  const mine = s.roles.length === 0 || s.roles.includes(user.role_code);
  const others = s.roles.filter((r) => r !== user.role_code);
  // 담당 부서가 하나뿐이고 내가 그 부서면 '가기'만. 여럿이면 '가기'와 함께 다른 부서로 다시 로그인(8단계 생성된 작업 확인 등)
  if (mine && others.length === 0) {
    return <Link to={s.to} className="btn small-btn">가기</Link>;
  }
  return (
    <div className="row">
      {mine && <Link to={s.to} className="btn small-btn">가기</Link>}
      {others.map((r) => {
        const a = accounts.find((x) => x.role_code === r);
        return (
          <button key={r} className="ghost small" onClick={() => onSwitch(r, s.to)}>
            {a ? `${a.name}(${a.login_id})` : r}로 다시 로그인
          </button>
        );
      })}
    </div>
  );
}

function who(s: FlowStep, accounts: Account[]): string {
  if (s.roles.length === 0) return s.n === 4 ? "해당 부서" : "엔진(자동)";
  return s.roles.map((r) => {
    const a = accounts.find((x) => x.role_code === r);
    return a ? `${a.dept_label} ${a.name}` : r;
  }).join(" · ");
}

// 표의 '누가' 칸은 부서만(이름·아이디는 툴팁) — 밀집도(이슈 #71)
function whoDept(s: FlowStep, accounts: Account[]): string {
  if (s.roles.length === 0) return s.n === 4 ? "해당 부서" : "엔진(자동)";
  return s.roles.map((r) => accounts.find((x) => x.role_code === r)?.dept_label ?? r).join(" · ");
}

// 시연 흐름 페이지
export function FlowPage({ v, user, onSwitch }: Props) {
  const accounts = useAccounts();
  const { steps, round, doneCount, now } = flowOf(v);
  const sim = v.session.kind === "sim";
  return (
    <>
      <Section title={`C-201 한 건 따라가기 · ${doneCount}/${steps.length}${round > 1 ? ` · 상신 ${round}회차` : ""}`} mode={["real", "mock"]}
        note={`현재 세션(${v.session.name})의 상태를 기준으로 표시합니다. ${sim ? "시뮬레이션 세션에서는 다른 부서 단계도 대행 입력할 수 있습니다." : "기준 세션에서는 각 단계를 해당 부서 계정으로 로그인해 입력합니다."} 바이어·이사회·부서 회신 값은 모의(고정 값)입니다.`}>
        <ol className="loop-strip" aria-label="이벤트 루프">
          {LOOP.map((x, i) => (
            <li key={x} className={now?.stage === i ? "on" : ""} aria-current={now?.stage === i ? "step" : undefined}>{x}</li>
          ))}
        </ol>
        {now ? (
          <p className="flow-now">
            <b>현재 {now.n}. {now.title}</b> — {who(now, accounts)}
            {now.roles.length > 0 && !now.roles.includes(user.role_code) && <span className="muted small"> · 현재 로그인: {user.name}({user.dept_label})</span>}
          </p>
        ) : (
          <EmptyState compact swan="success" title="모든 단계를 완료했습니다">현재 입찰 상태: {v.state.bid?.state}. 처음부터 다시 시연하려면 아래 안내에 따라 기준 세션을 초기화해 주세요.</EmptyState>
        )}
        <div className="scroll">
          <table className="grid text flow-table">
            <thead>
              <tr><th className="c-fit">#</th><th className="c-text-s">누가</th><th className="c-text">할 일</th><th className="c-fit">상태</th><th className="c-text-s">근거·결과</th><th className="c-fit"><span className="sr-only">이동</span></th></tr>
            </thead>
            <tbody>
              {steps.map((s) => (
                <tr key={s.n} className={`fs-${s.status}`} aria-current={s.status === "now" ? "step" : undefined}>
                  <td className="c-fit">{s.n}</td>
                  <td className="c-text-s small" title={who(s, accounts)}>{whoDept(s, accounts)}</td>
                  <td className="c-text" title={s.status === "now" || s.status === "open" ? undefined : s.desc}><b>{s.title}</b>{(s.status === "now" || s.status === "open") && <div className="muted small">{s.desc}</div>}</td>
                  <td className="c-fit"><span className={`fs-pill fs-pill-${s.status}`}>{s.n === 8 && s.status === "skip" ? "확인 안 함" : STATUS_LABEL[s.status]}</span></td>
                  <td className="c-text-s small">{s.evidence ?? ""}</td>
                  <td className="c-fit"><StepAction s={s} user={user} accounts={accounts} onSwitch={onSwitch} /></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Section>
      <details className="card restart-help">
        <summary><b>처음부터 다시 하려면</b></summary>
        <p className="small">
          기준 세션의 입력은 실제로 기록됩니다. 처음부터 다시 시연하려면 운영 관리 계정(admin01)으로 로그인한 뒤 <Link to="/sim">시뮬레이션</Link>에서 "이 세션 초기화"를 눌러 주세요.
          기준 세션을 그대로 두고 연습하려면 시뮬레이션 세션을 생성해 같은 순서로 진행하세요(대행 입력이 기록됩니다).
        </p>
      </details>
    </>
  );
}

// 모든 페이지 위의 한 줄: 지금 단계와 할 사람
export function FlowBar({ v, user, onSwitch }: Props) {
  // 맥락 줄 오른쪽에 들어간다(이슈 #73): 진행 수 · 지금 단계 · 할 사람(부서, 이름은 툴팁) · 이동 버튼
  const accounts = useAccounts();
  const { steps, doneCount, now } = flowOf(v);
  return (
    <span className="flowbar" role="note">
      <Link to="/flow" className="flowbar-title">시연 {doneCount}/{steps.length}</Link>
      {now ? (
        <>
          <span className="flowbar-now" title={`${now.n}. ${now.title} · ${who(now, accounts)}`}>지금 <b>{now.n}. {now.title}</b> · {whoDept(now, accounts)}</span>
          <StepAction s={now} user={user} accounts={accounts} onSwitch={onSwitch} />
        </>
      ) : (
        <span>모든 단계 완료 · 입찰 {v.state.bid?.state}</span>
      )}
    </span>
  );
}

// 로그인 화면의 시연 순서(상태 없이 계정 차례만)
// n은 시연 흐름 페이지의 단계 번호(2 엔진 자동, 4 다른 부서 회신은 로그인 차례가 아니라 빠진다)
export const FLOW_ORDER: { n: number; role: string; what: string }[] = [
  { n: 1, role: "sales", what: "RFP 접수" },
  { n: 3, role: "production", what: "확인 요청 회신" },
  { n: 5, role: "sales", what: "엔진 제안안 상신" },
  { n: 6, role: "board", what: "참여·대안 결정" },
  { n: 7, role: "sales", what: "제안 제출 → 작업 생성" },
  { n: 8, role: "production", what: "생성된 작업 확인" },
  { n: 9, role: "sales", what: "바이어 회신 → 재판단" },
];
