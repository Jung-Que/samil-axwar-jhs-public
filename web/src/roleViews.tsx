import type { Department, LaneAction, View } from "./types";
import { Section } from "./panels";
import { EVENT_LABEL } from "./format";
import { canEvent, ownsTask } from "./perm";

const FEATURE_LABEL: Record<string, string> = {
  view: "조회",
  "sim.session": "시뮬레이션 세션 만들기·분기·시연",
  "sim.act": "시뮬레이션 대행 입력·모의 가정",
  sweep: "민감도 격자·세션 비교",
  "main.clock": "기준 세션 시계",
  "main.reset": "기준 세션 초기화·시연",
  "session.delete": "시뮬레이션 세션 삭제",
};

// 부서 막대(이슈 #43): 사용자 부서를 고른다(모의 로그인). 권한은 부서별이고 서버가 최종 판단한다.
export function RoleBar({ v, dept, roleCode, onChoose }: { v: View; dept?: Department; roleCode: string; onChoose: (code: string) => void }) {
  const list = v.departments ?? [];
  const groups = [...new Set(list.map((d) => d.dept))];
  return (
    <div className={`rolebar ${dept ? "" : "no-role"}`}>
      <label className="field inline">
        <span>내 부서</span>
        <select value={roleCode} onChange={(e) => onChoose(e.target.value)}>
          <option value="">부서를 고른다</option>
          {groups.map((g) => (
            <optgroup key={g} label={g}>
              {list.filter((d) => d.dept === g).map((d) => <option key={d.code} value={d.code}>{d.label}</option>)}
            </optgroup>
          ))}
        </select>
      </label>
      {dept ? (
        <>
          <span className="chip strong">{dept.dept === dept.label ? dept.label : `${dept.dept} · ${dept.label}`}</span>
          <span className="muted small">{dept.duty}</span>
        </>
      ) : (
        <span className="small warn-text">부서를 고르면 그 부서 권한으로 입력할 수 있다(조회는 그대로).</span>
      )}
      <span className="muted small right">모의 로그인(부서 선택) · 인증은 서비스 단계</span>
    </div>
  );
}

// 내 업무: 부서 책임, 지금 할 수 있는 일, 나에게 온 확인 요청, 내 작업, 내 부서 입력 이력, 부서별 권한표.
export function MyWorkView({ v, dept, onAction, goRequests }: { v: View; dept?: Department; onAction: (a: LaneAction) => void; goRequests: () => void }) {
  if (!dept) {
    return (
      <Section title="내 업무" mode={["real"]} note="부서를 고르면 그 부서가 할 일과 권한이 보인다.">
        <PermissionTable v={v} />
      </Section>
    );
  }
  const reqs = (v.state.requests ?? []).filter((r) => r.to_role === dept.id && r.state !== "반영");
  const tasks = (v.state.tasks ?? []).filter((t) => t.state === "열림" && ownsTask(dept, t.owner_role));
  const actions = (v.lanes ?? [])
    .flatMap((l) => l.can ?? [])
    .filter((a) => {
      const req = (v.state.requests ?? []).find((r) => r.request_id === a.request_id);
      const c = canEvent(v, dept, a.event, req);
      return c.ok && !c.proxy;
    });
  const mine = [...(v.state.log ?? [])].reverse().filter((e) => e.event.actor_role === dept.id || e.event.by === dept.id).slice(0, 12);
  const sim = v.session.kind === "sim";
  return (
    <>
      <Section
        title={`내 업무 · ${dept.label}`}
        mode={["real"]}
        note={
          <>
            {dept.duty}. {sim ? "지금은 시뮬레이션 세션이라 다른 부서 일도 대행해 넣을 수 있다(입력 부서가 기록된다, [모의 대행])." : "지금은 기준 세션이라 자기 부서 권한으로만 넣는다."}
          </>
        }
      >
        <h3>지금 할 수 있는 일 ({actions.filter((a) => a.enabled).length})</h3>
        <div className="lane-actions">
          {actions.length === 0 && <span className="muted small">지금 내 부서 권한으로 할 사건이 없다</span>}
          {actions.map((a) => (
            <button key={a.label} className={a.enabled ? "small" : "ghost small"} disabled={!a.enabled} title={a.enabled ? "" : a.reason} onClick={() => onAction(a)}>
              {a.label}
            </button>
          ))}
        </div>
        <h3>나에게 온 확인 요청 ({reqs.length})</h3>
        {reqs.length === 0 ? (
          <p className="muted small">없음</p>
        ) : (
          <ul className="tasks">
            {reqs.map((r) => (
              <li key={r.request_id}>
                <span className="role">{r.request_id}</span> {r.question} <span className="muted small">기한 {r.due_date} · {r.state}</span>{" "}
                <button className="ghost small" onClick={goRequests}>회신하러 가기</button>
              </li>
            ))}
          </ul>
        )}
        <h3>내 작업 ({tasks.length})</h3>
        {tasks.length === 0 ? (
          <p className="muted small">없음</p>
        ) : (
          <ul className="tasks">{tasks.map((t) => <li key={t.task_id}>{t.title} <span className="muted small">{t.due_date ? `~${t.due_date}` : ""} · 사건 #{t.cause_seq}</span></li>)}</ul>
        )}
        <h3>내 부서 입력 이력 ({mine.length})</h3>
        {mine.length === 0 ? (
          <p className="muted small">없음</p>
        ) : (
          <ul className="tasks">
            {mine.map((e) => (
              <li key={e.event.seq}>
                #{e.event.seq} {EVENT_LABEL[e.event.type] ?? e.event.type}{" "}
                <span className="muted small">
                  권한 {e.event.actor_role}{e.event.by && e.event.by !== e.event.actor_role ? ` · 대행 입력 ${e.event.by}` : ""} · 업무일 {e.event.sim_date}
                </span>
              </li>
            ))}
          </ul>
        )}
      </Section>
      <Section title="부서별 권한표" mode={["real"]} note="서버가 이 표로 판단한다(server/w05/permissions.go). 기준 세션은 자기 부서 권한으로만, 시뮬레이션 세션은 대행 입력을 기록하며 허용한다.">
        <PermissionTable v={v} current={dept.code} />
      </Section>
    </>
  );
}

function PermissionTable({ v, current }: { v: View; current?: string }) {
  return (
    <table className="grid text">
      <thead><tr><th>부서</th><th>권한</th><th>기준 세션에서 넣는 사건</th><th>회신하는 확인 요청</th><th>기능</th></tr></thead>
      <tbody>
        {(v.departments ?? []).map((d) => (
          <tr key={d.code} className={d.code === current ? "row-strong" : ""}>
            <td className="nowrap">{d.dept}</td>
            <td className="nowrap">{d.label}</td>
            <td className="small">{(d.events ?? []).filter((e) => e !== "E3_REPLY").map((e) => EVENT_LABEL[e] ?? e).join(" · ") || "-"}</td>
            <td className="small">{(d.replies ?? []).join(" · ") || "-"}</td>
            <td className="small">{d.features.map((f) => FEATURE_LABEL[f] ?? f).join(" · ")}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
