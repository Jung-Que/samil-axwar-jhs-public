import type { AiCall, View } from "./types";

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
  event: (type: string, actor_role: string, payload: unknown, ai_call?: AiCall | null) =>
    call<View>("POST", "/api/w05/events", { type, actor_role, payload, ai_call: ai_call ?? undefined }),
  structure: (kind: string, text: string) => call<AiCall>("POST", "/api/w05/ai/structure", { kind, text }),
  reset: () => call<View>("POST", "/api/w05/reset"),
  scenario: (id: string) => call<View>("POST", `/api/w05/scenarios/${id}`),
};
