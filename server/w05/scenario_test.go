package w05

import (
	"context"
	"strings"
	"testing"

	"axwar/server/store"
)

// 명세 §10 시연 3건의 기대 상태·작업. 사건을 실제로 적용해 확인한다(녹화 AI 응답 사용).

func runScenario(t *testing.T, id string) *State {
	t.Helper()
	t.Setenv("ANTHROPIC_API_KEY", "")
	b := loadBase(t)
	svc, err := NewService(context.Background(), b, store.NewMemory(), NewAIFromEnv())
	if err != nil {
		t.Fatal(err)
	}
	v, err := svc.RunScenario(context.Background(), id)
	if err != nil {
		t.Fatalf("시나리오 %s: %v", id, err)
	}
	return v.State
}

func line(s *State, p string) *BidLine {
	for _, l := range s.Lines {
		if l.ProductID == p {
			return l
		}
	}
	return nil
}

func hasCond(l *BidLine, code string) bool {
	for _, c := range l.Conditions {
		if c.Code == code {
			return true
		}
	}
	return false
}

func openTask(s *State, prefix string) bool {
	for _, t := range s.Tasks {
		if t.State == "열림" && strings.HasPrefix(t.Title, prefix) {
			return true
		}
	}
	return false
}

func reqState(s *State, id string) string {
	if r := s.request(id); r != nil {
		return r.State
	}
	return "없음"
}

func TestScenarioNormal(t *testing.T) {
	s := runScenario(t, "normal")
	wantS(t, "입찰 상태", s.Bid.State, BidAwaiting)
	wantS(t, "PL-01 상태", line(s, ProductBasic).QtyStatus, "미확정")
	p2 := line(s, ProductDerma)
	wantS(t, "PL-02 상태", p2.QtyStatus, "제안됨")
	wantS(t, "PL-02 첫 달", p2.Schedule[0].Month, "2026-10")
	wantInt(t, "PL-02 첫 달 수량", p2.Schedule[0].Qty, 50_000)
	// 2026-10~2027-04 OPT-02 월 50,000 × 7 + 2027-05~09 OPT-01 합류 125,000 × 5
	wantInt(t, "PL-02 제안 합계", p2.ProposedTotal, 975_000)
	for _, c := range []string{"COND_OPT02_F2", "COND_OPT01_STAGED", "COND_NO_PRE_QUAL"} {
		if !hasCond(p2, c) {
			t.Errorf("PL-02 조건 %s 없음", c)
		}
	}
	wantS(t, "OPT-02", s.Scenario.Options["OPT-02"].Status, OptApproved)
	wantS(t, "OPT-01", s.Scenario.Options["OPT-01"].Status, OptDeferred)
	wantS(t, "결정 신선도", s.Decisions[0].Freshness, "최신")
	if len(s.Requests) != 7 {
		t.Errorf("확인 요청 %d건, 기대 7건(CR-01~07)", len(s.Requests))
	}
	for _, p := range []string{"생산계획 잠정 반영", "300억 배분 조건 반영", "바이어 회신 추적"} {
		if !openTask(s, p) {
			t.Errorf("열린 작업 %q 없음", p)
		}
	}
	if openTask(s, "제안서 작성") {
		t.Error("제출 뒤에도 '제안서 작성'이 열려 있음")
	}
	wantInt(t, "가예약 건수", int64(len(s.Reservations)), 12)
	if s.Log[2].Event.AiCall == nil || s.Log[2].Event.AiCall.Mode != "recorded" {
		t.Error("이사회 회신이 A1(녹화)으로 구조화되지 않음")
	}
}

func TestScenarioChange(t *testing.T) {
	s := runScenario(t, "change")
	wantS(t, "입찰 상태", s.Bid.State, BidAwaiting)
	wantInt(t, "입찰 버전", int64(s.Bid.Version), 2)
	wantS(t, "첫 결정", s.Decisions[0].Freshness, "낡음")
	wantS(t, "둘째 결정", s.Decisions[1].Freshness, "최신")
	wantF(t, "단가 인하", s.Scenario.PriceCut, -0.07)
	wantS(t, "페널티 기준", s.Scenario.PenaltyBasis, "contract")
	wantS(t, "CR-08(Masil SCM)", reqState(s, "CR-08"), "요청")
	wantS(t, "CR-04(개시일)", reqState(s, "CR-04"), "반영")
	wantS(t, "CR-05(페널티)", reqState(s, "CR-05"), "반영")
	wantS(t, "CR-06(생산지)", reqState(s, "CR-06"), "요청")
	if !hasCond(line(s, ProductDerma), "COND_MASIL_PROTECTED") {
		t.Error("PL-02에 Masil 보호 조건 없음")
	}
	wantInt(t, "첫 달 부족(Masil 보호로 미확정)", s.Eval.InitialGap, 30_000)
	// 바이어 회신 직후 결정이 낡고 재상신 작업이 생겼다가, 재상신으로 닫혀야 한다
	stale := false
	for _, e := range s.Log {
		for _, c := range e.Changes {
			if strings.Contains(c, "낡음") {
				stale = true
			}
		}
	}
	if !stale {
		t.Error("결정 낡음 기록 없음")
	}
	if openTask(s, "재상신") {
		t.Error("재상신 뒤에도 '재상신' 작업이 열려 있음")
	}
}

func TestScenarioReject(t *testing.T) {
	s := runScenario(t, "reject")
	wantS(t, "입찰 상태", s.Bid.State, BidAwaiting)
	wantS(t, "OPT-01", s.Scenario.Options["OPT-01"].Status, OptRejected)
	for _, id := range []string{"CR-01", "CR-02", "CR-03"} {
		wantS(t, id, reqState(s, id), "기한 초과")
	}
	if !openTask(s, "재요청: CR-02") {
		t.Error("CR-02 재요청 작업 없음")
	}
	if !openTask(s, "바이어 범위 축소 협상") {
		t.Error("바이어 범위 축소 협상 작업 없음")
	}
	p2 := line(s, ProductDerma)
	wantInt(t, "PL-02 제안 합계(OPT-02 범위)", p2.ProposedTotal, 600_000)
	wantS(t, "PL-01 상태", line(s, ProductBasic).QtyStatus, "미확정")
	if hasCond(p2, "COND_OPT01_STAGED") {
		t.Error("거절된 OPT-01 조건이 남아 있음")
	}
}

// 잘못된 사건은 상태를 바꾸지 않고, 차단된 제출은 기록만 남긴다.
func TestInvalidAndBlocked(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	b := loadBase(t)
	svc, err := NewService(context.Background(), b, store.NewMemory(), NewAIFromEnv())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := svc.Submit(ctx, EvForApprove, "영업", nil, nil); err == nil {
		t.Fatal("입찰 접수 전 상신이 통과함")
	}
	if _, err := svc.RunScenario(ctx, "normal"); err != nil {
		t.Fatal(err)
	}
	// 단가 -12% 회신 → 결정 낡음 → 재상신·재승인(하한 -10%) 뒤 제출하면 COND_PRICE_FLOOR로 차단
	steps := []struct {
		typ, actor, payload string
	}{
		{EvBuyer, "영업", `{"price_cut_pct":-12}`},
		{EvForApprove, "영업", `{}`},
		{EvDecision, "CEO/이사회", `{"participation":"조건부","options":{"OPT-02":"승인","OPT-01":"보류"},"conditions":["COND_PL01_UNCONFIRMED"],"price_floor_pct":-10}`},
		{EvSubmit, "영업", `{}`},
	}
	var v View
	for _, st := range steps {
		if v, err = svc.Submit(ctx, st.typ, st.actor, []byte(st.payload), nil); err != nil {
			t.Fatalf("%s: %v", st.typ, err)
		}
	}
	if len(v.State.LastBlocks) == 0 || !strings.Contains(v.State.LastBlocks[0], "COND_PRICE_FLOOR") {
		t.Fatalf("단가 하한 차단 없음: %v", v.State.LastBlocks)
	}
	wantS(t, "차단 뒤 입찰 상태", v.State.Bid.State, BidConditional)
}
