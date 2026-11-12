import type { ConfirmRequest, Department, View } from "./types";

// 부서별 권한(이슈 #43). 서버(server/w05/permissions.go)가 최종 판단하고, 화면은 같은 권한표로 버튼을 막고 이유를 보인다.
// 기준 세션: 자기 부서 권한으로만 입력. 시뮬레이션 세션: 다른 부서 대행 입력 허용(입력 부서가 기록된다).

export type Check = { ok: boolean; reason?: string; proxy?: boolean };

export function deptOf(v: View, code: string): Department | undefined {
  return (v.departments ?? []).find((d) => d.code === code);
}

export function ownersOf(v: View, type: string): string {
  return (v.departments ?? []).filter((d) => (d.events ?? []).includes(type)).map((d) => d.label).join("·") || "시스템";
}

// canEvent는 지금 세션에서 이 부서가 이 사건을 넣을 수 있는지 본다.
export function canEvent(v: View, d: Department | undefined, type: string, req?: ConfirmRequest): Check {
  if (!d) return { ok: false, reason: "부서를 먼저 고른다" };
  const responder = type === "E3_REPLY" ? req?.to_role : undefined;
  const own = type === "E3_REPLY" ? responder === d.id : (d.events ?? []).includes(type);
  if (v.session.kind === "sim") {
    if (!d.features.includes("sim.act")) return { ok: false, reason: `${d.label} 부서는 시뮬레이션 입력 권한이 없다` };
    return own ? { ok: true } : { ok: true, proxy: true, reason: `대행: ${responder ?? ownersOf(v, type)} 권한(시뮬레이션)` };
  }
  if (own) return { ok: true };
  return { ok: false, reason: `${responder ?? ownersOf(v, type)} 권한 — 대행은 시뮬레이션 세션에서` };
}

export function canFeature(d: Department | undefined, feat: string): boolean {
  return !!d && d.features.includes(feat);
}

// 작업 담당 이름을 부서 ID로 맞춘다("그룹 재무" 작업은 재무·전략 부서).
export function ownsTask(d: Department | undefined, owner: string): boolean {
  if (!d) return false;
  return owner === d.id || (d.id === "재무·전략" && owner === "그룹 재무");
}
