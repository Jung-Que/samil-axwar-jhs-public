package w05

import (
	"encoding/json"
	"fmt"
)

// 시연 시나리오(명세 §10). LLM은 현재 제외라(11/10 사용자 결정) 모든 사건은 한 번 정리된 고정 수치로 넣는다.
// 바이어·이사회·부서 회신은 [모의]다. 값은 원본 문서(rfp_c201 등)와 업무모델 명세에서 정했다.

// RFPFromSource는 rfp_c201.md 표와 조건을 옮긴 고정 값이다(E1 입력).
func RFPFromSource() RFPFields {
	return RFPFields{Items: DefaultItems(), Currency: "USD", PenaltyRate: 0.03, RecallBearer: "supplier", DeadlineWeeks: 3}
}

// Step은 시나리오의 한 단계(사건 하나와 그 고정 입력값)다.
type Step struct {
	Type    string `json:"type"`
	Actor   string `json:"actor_role"`
	Label   string `json:"label"`
	Payload any    `json:"payload"`
}

// ScenarioInfo는 화면에 보여 줄 시나리오 설명이다. 화면은 단계의 고정 값을 사건 입력 프리셋으로도 쓴다.
type ScenarioInfo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Desc  string `json:"description"`
	Steps []Step `json:"steps"`
}

func stepsFor(asOf string) (normal, change, reject []Step) {
	sc := SessionScenario(asOf)
	rfp := Step{Type: EvRFP, Actor: "영업", Label: "RFP 원본 값(rfp_c201)", Payload: RFPFromSource()}
	forApprove := Step{Type: EvForApprove, Actor: "영업", Label: "엔진 제안안 상신", Payload: map[string]any{"note": "엔진 제안안 상신"}}
	submit := Step{Type: EvSubmit, Actor: "영업", Label: "제안 제출", Payload: map[string]any{}}
	decisionNormal := Step{Type: EvDecision, Actor: "CEO/이사회", Label: "조건부 승인: OPT-02 즉시, OPT-01 보류, 인하 하한 -10%",
		Payload: DecisionFields{Participation: "조건부", Options: map[string]string{"OPT-01": OptDeferred, "OPT-02": OptApproved},
			Conditions:    []string{"COND_PL01_UNCONFIRMED", "COND_NO_PRE_QUAL", "COND_OPT02_F2", "COND_OPT01_STAGED"},
			PriceFloorPct: f64(-10), Note: "조건부 승인: OPT-02 즉시, OPT-01은 수주 확정·개시일 합의 뒤 재상신, PL-01 미확정"}}
	buyerChange := Step{Type: EvBuyer, Actor: "영업",
		Label:   fmt.Sprintf("바이어 회신: 초도 %s 80,000 EA, 분할 허용, 단가 -7%%, 계약 총액 기준 페널티", sc.DeliveryStart),
		Payload: BuyerReply{DeliveryStart: str(sc.DeliveryStart), SplitAllowed: boolp(true), PriceCutPct: f64(-7), PenaltyBasis: str("contract"), InitialQtyRequired: i64(80_000)}}
	decisionChange := Step{Type: EvDecision, Actor: "CEO/이사회", Label: "재승인: -7% 수용, Masil 보호분 침범 금지",
		Payload: DecisionFields{Participation: "조건부", Options: map[string]string{"OPT-01": OptDeferred, "OPT-02": OptApproved},
			Conditions:    []string{"COND_PL01_UNCONFIRMED", "COND_NO_PRE_QUAL", "COND_OPT02_F2", "COND_OPT01_STAGED", "COND_MASIL_PROTECTED"},
			PriceFloorPct: f64(-10), Note: "-7% 인하 수용, Masil 공급분 침범 금지, PL-01 미확정 유지"}}
	decisionReject := Step{Type: EvDecision, Actor: "CEO/이사회", Label: "조건부: OPT-01 거절, 더마는 OPT-02 범위로",
		Payload: DecisionFields{Participation: "조건부", Options: map[string]string{"OPT-01": OptRejected, "OPT-02": OptApproved},
			Conditions: []string{"COND_PL01_UNCONFIRMED", "COND_NO_PRE_QUAL", "COND_OPT02_F2"}, Note: "OPT-01 거절, 더마는 OPT-02 범위로 축소, PL-01 미확정"}}
	clock := fmtDate(mustDate(asOf).AddDate(0, 0, 8)) // 생산관리 확인 요청 기한(접수 + 5영업일)을 넘기는 날

	normal = []Step{rfp, forApprove, decisionNormal, submit}
	change = append(append([]Step(nil), normal...), buyerChange,
		Step{Type: EvForApprove, Actor: "영업", Label: "바뀐 조건으로 재상신", Payload: map[string]any{"note": "바뀐 조건으로 재상신"}},
		decisionChange, submit)
	reject = []Step{rfp, forApprove,
		{Type: EvClock, Actor: "시스템", Label: fmt.Sprintf("업무일 %s(기준일 + 8일, 생산관리 회신 기한 경과)", clock), Payload: map[string]any{"sim_date": clock}},
		decisionReject, submit}
	return
}

// Scenarios는 기준일 asOf에서 시작하는 시연 3건이다(날짜는 기준일 상대). asOf가 비면 2026-07-01.
func Scenarios(asOf string) []ScenarioInfo {
	if asOf == "" {
		asOf = DefaultAsOf
	}
	normal, change, reject := stepsFor(asOf)
	return []ScenarioInfo{
		{ID: "normal", Title: "정상", Desc: "RFP 접수 → 상신 → 이사회 조건부 승인(OPT-02 즉시, OPT-01 보류) → 점검표 통과 후 제출", Steps: normal},
		{ID: "change", Title: "변경", Desc: "정상 이후 바이어 회신(초도 80,000 EA, 단가 -7%, 계약 총액 기준 페널티) → 결정 낡음 → Masil 보호 충돌 확인 요청 → 재상신·재승인·제출", Steps: change},
		{ID: "reject", Title: "정보 부족·거절", Desc: "생산관리 회신 기한 초과(PL-01·단위·Masil) → 이사회가 OPT-01 거절 → 더마를 OPT-02 범위(40%)로 축소해 제출", Steps: reject},
	}
}

// ScenarioByID는 ID로 시나리오를 찾는다.
func ScenarioByID(id, asOf string) (ScenarioInfo, error) {
	for _, s := range Scenarios(asOf) {
		if s.ID == id {
			return s, nil
		}
	}
	return ScenarioInfo{}, fmt.Errorf("모르는 시나리오: %s", id)
}

func stepJSON(s Step) (json.RawMessage, error) { return json.Marshal(s.Payload) }

func f64(v float64) *float64 { return &v }
func i64(v int64) *int64     { return &v }
func str(v string) *string   { return &v }
func boolp(v bool) *bool     { return &v }
