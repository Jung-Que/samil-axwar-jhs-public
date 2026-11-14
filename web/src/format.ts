// 숫자·라벨 형식. 수량은 EA 정수, 금액은 억 원(소수 2자리).

export const ea = (n: number | undefined | null) => (n ?? 0).toLocaleString("ko-KR");
export const eok = (krw: number | undefined | null, nd = 2) =>
  `${((krw ?? 0) / 1e8).toLocaleString("ko-KR", { minimumFractionDigits: nd, maximumFractionDigits: nd })}억`;
export const pct = (v: number | undefined | null, nd = 1) => `${(v ?? 0).toFixed(nd)}%`;
export const won = (v: number | undefined | null) => `${Math.round(v ?? 0).toLocaleString("ko-KR")}원`;

export const EVENT_LABEL: Record<string, string> = {
  E0_CLOCK: "E0 시계",
  E1_RFP: "E1 RFP 접수",
  E2_DECISION: "E2 승인 결정",
  E9_SIM_ASSUMPTION: "E9 모의 가정",
  E3_REPLY: "E3 부서 회신",
  E4_BUYER_REPLY: "E4 바이어 회신",
  E5_NEW_ORDER: "E5 신규 수주",
  E7_SUBMIT_PROPOSAL: "E7 제안 제출",
  E8_SUBMIT_APPROVAL: "E8 상신",
};

export const BASIS_LABEL: Record<string, string> = {
  base: "기존 라인 여유",
  "OPT-02": "OPT-02 F2 전환",
  "OPT-01": "OPT-01 L-02 증설",
  "MASIL-ADJ": "Masil 조정분",
  "CUST-REL": "이탈 고객 반환(영업 임원 확인, R13)",
};

// 상태 이름을 CSS 클래스로 쓸 때 띄어쓰기를 없앤다("기한 초과" → "기한-초과").
export const slug = (s: string) => s.replace(/\s+/g, "-");

export const PRODUCT_LABEL: Record<string, string> = { "PL-01": "PL-01 기초", "PL-02": "PL-02 더마" };
