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

// 사용자 부서(이슈 #43). 화면에서 고르는 부서 선택이고, 서버로는 영문 코드를 X-W05-Role 헤더로 보낸다.
// 시연용 롤 체크라 로그인·인증은 없다(사용자 결정 11/13). 쓰기 요청은 부서가 없으면 서버가 401을 돌려준다.
const ROLE_KEY = "w05.role";
let role = (() => {
  const fromUrl = new URLSearchParams(window.location.search).get("role"); // 시연용 링크: ?role=production 등
  if (fromUrl) return fromUrl;
  try {
    return localStorage.getItem(ROLE_KEY) || "";
  } catch {
    return "";
  }
})();

export function currentRole(): string {
  return role;
}

export function setRole(code: string) {
  role = code;
  try {
    localStorage.setItem(ROLE_KEY, code);
  } catch {
    /* 저장 불가여도 이번 화면에서는 쓴다 */
  }
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {};
  if (body !== undefined) headers["Content-Type"] = "application/json";
  if (role) headers["X-W05-Role"] = role;
  const r = await fetch(path, {
    method,
    headers,
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

// 지금 세션 종류. 기준 세션은 자기 부서 권한으로만 넣으므로 actor를 비워 서버가 사용자 부서를 쓰게 하고,
// 시뮬레이션 세션은 그 사건의 본래 부서(대행)를 보낸다.
let sessionKind: "main" | "sim" = "main";

export function setSession(id: string, kind?: "main" | "sim") {
  if (kind) sessionKind = kind;
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
  // 사건은 고정 필드로만 넣는다(자유 문장·LLM 현재 제외). owner는 그 사건의 본래 부서(시뮬레이션 대행에 쓴다).
  event: (type: string, owner: string, payload: unknown) =>
    call<View>("POST", withSession("/api/w05/events"), { type, actor_role: sessionKind === "sim" ? owner : "", payload }),
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
