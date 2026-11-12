import type { Backtest, Inbox, SessionCompare, SweepAxis, SweepResult, View } from "./types";

// API 오류. 규칙 위반(409)이면 서버가 현재 상태(view)를 함께 보낸다.
export class ApiError extends Error {
  view?: View;
  status: number;
  constructor(message: string, status: number, view?: View) {
    super(message);
    this.view = view;
    this.status = status;
  }
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const r = await fetch(path, {
    method,
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const data = await r.json().catch(() => ({}));
  if (!r.ok) throw new ApiError(data.error ?? `HTTP ${r.status}`, r.status, data.view);
  return data as T;
}

// 지금 보는 세션(이슈 #37). 서버 응답의 session.id로 맞추고, 새로고침해도 이어 보도록 브라우저에 기억한다(없어도 동작).
// 주소에 ?session=이 있으면 그 세션을 먼저 연다(세션 링크 공유).
const KEY = "w05.session";
let session = (() => {
  const fromUrl = new URLSearchParams(window.location.search).get("session");
  if (fromUrl) return fromUrl;
  try {
    return localStorage.getItem(KEY) || "main";
  } catch {
    return "main";
  }
})();

export function currentSession(): string {
  return session;
}

export function setSession(id: string) {
  session = id || "main";
  try {
    localStorage.setItem(KEY, session);
  } catch {
    /* 저장 불가(사생활 보호 모드 등)여도 이번 화면에서는 쓴다 */
  }
}

const withSession = (path: string, id = session) => `${path}${path.includes("?") ? "&" : "?"}session=${encodeURIComponent(id)}`;

export const api = {
  state: () => call<View>("GET", withSession("/api/w05/state")),
  // 사건은 고정 필드로만 넣는다(자유 문장·LLM 현재 제외).
  event: (type: string, actor_role: string, payload: unknown) => call<View>("POST", withSession("/api/w05/events"), { type, actor_role, payload }),
  // 지금 세션을 비운다. as_of가 있으면 그 기준일로 다시 시작한다.
  reset: (as_of?: string) => call<View>("POST", withSession("/api/w05/reset"), as_of ? { as_of } : {}),
  // 시연: newSession이면 새 시뮬레이션 세션에서 돌린다(기존 세션 보존).
  scenario: (id: string, as_of?: string, newSession = true) =>
    call<View>("POST", withSession(`/api/w05/scenarios/${id}`), { ...(as_of ? { as_of } : {}), new_session: newSession }),
  inbox: (days: number, limit = 300) => call<Inbox>("GET", withSession(`/api/w05/inbox?days=${days}&limit=${limit}`)),
  backtest: () => call<Backtest>("GET", withSession("/api/w05/backtest")),
  // 세션
  open: (id: string) => call<View>("GET", withSession("/api/w05/state", id)),
  newSession: (name: string, as_of?: string) => call<View>("POST", "/api/w05/sessions", { name, ...(as_of ? { as_of } : {}) }),
  fork: (seq: number, name = "") => call<View>("POST", `/api/w05/sessions/${encodeURIComponent(session)}/fork`, { seq, name }),
  deleteSession: (id: string) => call<View>("DELETE", `/api/w05/sessions/${encodeURIComponent(id)}`),
  compare: (a: string, b: string) => call<SessionCompare>("GET", `/api/w05/compare?a=${encodeURIComponent(a)}&b=${encodeURIComponent(b)}`),
  // 민감도 격자: 사건 없이 지금 세션 시나리오로 엔진 평가만 반복한다.
  sweep: (x: SweepAxis, y?: SweepAxis) => call<SweepResult>("POST", withSession("/api/w05/sweep"), { x, ...(y ? { y } : {}) }),
};
