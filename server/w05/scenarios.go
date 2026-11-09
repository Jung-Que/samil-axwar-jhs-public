package w05

import (
	"encoding/json"
	"fmt"
)

// 시연 시나리오(명세 §10). 회신 문장은 모두 [모의]이고, 문장 → 필드 변환은 A1(AI)이 맡는다.
// 시나리오 실행은 재현을 위해 녹화 응답을 먼저 쓴다. 화면에서 직접 넣은 문장은 키가 있으면 실제 호출한다.

// 모의 회신 문장. 녹화 응답(recordedResponses)의 열쇠이기도 하다.
const (
	TextDecisionNormal = `C-201 참여는 조건부로 승인한다. OPT-02(F2 더마 전환, 12억)는 즉시 승인한다. OPT-01(F1 더마 증설, 180억)은 C-201 수주가 확정되고 납품 개시일이 합의된 뒤 다시 올려라. 기초(PL-01) 수량은 생산관리 확인 전에는 확정하지 말고, 증설·전환 능력은 재검증이 끝나기 전에 약속하지 말 것. 단가 인하는 -10%까지만 허용한다.`

	TextBuyerChange = `검토해 주셔서 감사합니다. 초도 납품은 2026년 10월부터 원하며, 10월 더마 물량 80,000 EA는 반드시 필요합니다. 분할 납품은 가능합니다. 경쟁 입찰 결과를 반영해 단가 7% 인하를 요청드립니다. 납기 지연 페널티는 연 계약 총액 기준으로 적용합니다. 해외공장 생산 여부는 추후 회신드리겠습니다.`

	TextDecisionChange = `단가 7% 인하는 승인 하한(-10%) 안이므로 조건부 승인을 유지한다. OPT-02는 승인 상태를 유지하고 OPT-01은 계속 보류한다. 첫 달 80,000 EA 가운데 계열사 Masil 공급분을 건드려야 하는 수량은 쓰지 말고 미확정으로 제안하라. 기초 수량은 여전히 확정하지 말 것.`

	TextDecisionReject = `C-201은 조건부로 진행한다. 다만 OPT-01 증설 180억은 승인하지 않는다. 더마는 OPT-02 범위에서만 제안하라. OPT-02는 승인한다. 기초(PL-01)는 생산관리 회신이 없으므로 수량을 확정하지 말 것. 전환 능력은 재검증 전에 약속하지 말 것.`
)

// recordedResponses는 위 문장과 RFP 원문에 대한 A1 녹화 응답이다(키가 없을 때 [모의]로 재생).
func recordedResponses() map[string]json.RawMessage {
	m := map[string]json.RawMessage{}
	put := func(kind, text string, v any) {
		b, err := json.Marshal(v)
		if err != nil {
			panic(err)
		}
		m[textKey(kind, text)] = b
	}
	put(KindRFP, RawText("01_corevia/docs/rfp_c201.md"), RFPFields{Items: DefaultItems(), Currency: "USD", PenaltyRate: 0.03, RecallBearer: "supplier", DeadlineWeeks: 3})
	put(KindDecision, TextDecisionNormal, DecisionFields{Participation: "조건부", Options: map[string]string{"OPT-01": OptDeferred, "OPT-02": OptApproved},
		Conditions: []string{"COND_PL01_UNCONFIRMED", "COND_NO_PRE_QUAL", "COND_OPT02_F2", "COND_OPT01_STAGED"}, PriceFloorPct: f64(-10),
		Note: "조건부 승인: OPT-02 즉시, OPT-01은 수주 확정·개시일 합의 뒤 재상신, PL-01 미확정"})
	put(KindBuyer, TextBuyerChange, BuyerReply{DeliveryStart: str("2026-10"), SplitAllowed: boolp(true), PriceCutPct: f64(-7), PenaltyBasis: str("contract"),
		InitialQtyRequired: i64(80_000)})
	put(KindDecision, TextDecisionChange, DecisionFields{Participation: "조건부", Options: map[string]string{"OPT-01": OptDeferred, "OPT-02": OptApproved},
		Conditions: []string{"COND_PL01_UNCONFIRMED", "COND_NO_PRE_QUAL", "COND_OPT02_F2", "COND_OPT01_STAGED", "COND_MASIL_PROTECTED"}, PriceFloorPct: f64(-10),
		Note: "-7% 인하 수용, Masil 공급분 침범 금지, PL-01 미확정 유지"})
	put(KindDecision, TextDecisionReject, DecisionFields{Participation: "조건부", Options: map[string]string{"OPT-01": OptRejected, "OPT-02": OptApproved},
		Conditions: []string{"COND_PL01_UNCONFIRMED", "COND_NO_PRE_QUAL", "COND_OPT02_F2"}, Note: "OPT-01 거절, 더마는 OPT-02 범위로 축소, PL-01 미확정"})
	return m
}

// Step은 시나리오의 한 단계다. Text가 있으면 A1으로 구조화한 결과를 사건 입력으로 쓴다.
type Step struct {
	Type    string `json:"type"`
	Actor   string `json:"actor_role"`
	Kind    string `json:"kind,omitempty"`
	Text    string `json:"text,omitempty"`
	Payload any    `json:"payload,omitempty"`
}

// ScenarioInfo는 화면에 보여 줄 시나리오 설명이다.
type ScenarioInfo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Desc  string `json:"description"`
	Steps []Step `json:"steps"`
}

func normalSteps() []Step {
	return []Step{
		{Type: EvRFP, Actor: "영업", Kind: KindRFP, Text: RawText("01_corevia/docs/rfp_c201.md")},
		{Type: EvForApprove, Actor: "영업", Payload: map[string]any{"note": "엔진 제안안 상신"}},
		{Type: EvDecision, Actor: "CEO/이사회", Kind: KindDecision, Text: TextDecisionNormal},
		{Type: EvSubmit, Actor: "영업", Payload: map[string]any{}},
	}
}

// Scenarios는 시연 3건이다.
func Scenarios() []ScenarioInfo {
	change := append(normalSteps(),
		Step{Type: EvBuyer, Actor: "영업", Kind: KindBuyer, Text: TextBuyerChange},
		Step{Type: EvForApprove, Actor: "영업", Payload: map[string]any{"note": "바뀐 조건으로 재상신"}},
		Step{Type: EvDecision, Actor: "CEO/이사회", Kind: KindDecision, Text: TextDecisionChange},
		Step{Type: EvSubmit, Actor: "영업", Payload: map[string]any{}},
	)
	reject := []Step{
		{Type: EvRFP, Actor: "영업", Kind: KindRFP, Text: RawText("01_corevia/docs/rfp_c201.md")},
		{Type: EvForApprove, Actor: "영업", Payload: map[string]any{"note": "엔진 제안안 상신"}},
		{Type: EvClock, Actor: "시스템", Payload: map[string]any{"sim_date": "2026-07-09"}},
		{Type: EvDecision, Actor: "CEO/이사회", Kind: KindDecision, Text: TextDecisionReject},
		{Type: EvSubmit, Actor: "영업", Payload: map[string]any{}},
	}
	return []ScenarioInfo{
		{ID: "normal", Title: "정상", Desc: "RFP 접수 → 상신 → 이사회 조건부 승인(OPT-02 즉시, OPT-01 보류) → 점검표 통과 후 제출", Steps: normalSteps()},
		{ID: "change", Title: "변경", Desc: "정상 이후 바이어 회신(초도 10월 80,000 EA, 단가 -7%, 계약 총액 기준 페널티) → 결정 낡음 → Masil 보호 충돌 확인 요청 → 재상신·재승인·제출", Steps: change},
		{ID: "reject", Title: "정보 부족·거절", Desc: "생산관리 회신 기한 초과(PL-01·단위·Masil) → 이사회가 OPT-01 거절 → 더마를 OPT-02 범위(40%)로 축소해 제출", Steps: reject},
	}
}

// ScenarioByID는 ID로 시나리오를 찾는다.
func ScenarioByID(id string) (ScenarioInfo, error) {
	for _, s := range Scenarios() {
		if s.ID == id {
			return s, nil
		}
	}
	return ScenarioInfo{}, fmt.Errorf("모르는 시나리오: %s", id)
}

func f64(v float64) *float64 { return &v }
func i64(v int64) *int64     { return &v }
func str(v string) *string   { return &v }
func boolp(v bool) *bool     { return &v }
