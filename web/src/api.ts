import type { Backtest, Inbox, View } from "./types";

// API 오류. 규칙 위반(409)이면 서버가 현재 상태(view)를 함께 보낸다.
export class ApiError extends Error {
  view?: View;
  constructor(message: string, view?: View) {
    super(message);
    this.view = view;
  }
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const r = await fetch(path, {
    method,
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const data = await r.json().catch(() => ({}));
  if (!r.ok) throw new ApiError(data.error ?? `HTTP ${r.status}`, data.view);
  return data as T;
}

export const api = {
  state: () => call<View>("GET", "/api/w05/state"),
  // 사건은 고정 필드로만 넣는다(자유 문장·LLM 현재 제외).
  event: (type: string, actor_role: string, payload: unknown) => call<View>("POST", "/api/w05/events", { type, actor_role, payload }),
  // as_of가 있으면 그 기준일로 테스트 세션을 시작한다(없으면 지금 세션 기준일).
  reset: (as_of?: string) => call<View>("POST", "/api/w05/reset", as_of ? { as_of } : {}),
  scenario: (id: string, as_of?: string) => call<View>("POST", `/api/w05/scenarios/${id}`, as_of ? { as_of } : {}),
  inbox: (days: number, limit = 300) => call<Inbox>("GET", `/api/w05/inbox?days=${days}&limit=${limit}`),
  backtest: () => call<Backtest>("GET", "/api/w05/backtest"),
};
