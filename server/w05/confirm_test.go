package w05

import (
	"context"
	"errors"
	"testing"

	"axwar/server/store"
)

// 이슈 #35: 원본에 없는 정보는 확인 요청(CR-10 라인별 전환 조건, CR-11 주문별 확정 납기)으로 남긴다.
// 회신은 기록하고 후속 작업을 만들지만 엔진 계산은 바꾸지 않는다(설계 단계).

func TestConfirmRequestsRecordOnly(t *testing.T) {
	ctx := context.Background()
	b := loadBase(t)
	svc, err := NewService(ctx, b, store.NewMemory())
	if err != nil {
		t.Fatal(err)
	}
	v, err := svc.RunScenario(ctx, "normal", "")
	if err != nil {
		t.Fatal(err)
	}
	before := v.State.Eval
	reply := func(actor, id, answer string) (View, error) {
		return svc.Submit(ctx, EvReply, actor, []byte(`{"request_id":"`+id+`","answer":`+answer+`}`))
	}
	// CR-10: PL-01 후보가 아닌 라인(L-02 더마 전용, L-01 자기 라인)과 여유 초과는 거절
	for _, bad := range []string{
		`{"lines":["L-02"],"changeover_weeks":4,"annual_qty":1000}`,
		`{"lines":["L-01"],"changeover_weeks":4,"annual_qty":1000}`,
		`{"lines":["L-03"],"changeover_weeks":4,"annual_qty":293043}`,
	} {
		if _, err := reply("생산관리", "CR-10", bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("CR-10 잘못된 회신이 통과: %s (%v)", bad, err)
		}
	}
	v, err = reply("생산관리", "CR-10", `{"lines":["L-03","L-11"],"changeover_weeks":4,"annual_qty":300000}`)
	if err != nil {
		t.Fatalf("CR-10 회신 거절: %v", err)
	}
	wantS(t, "CR-10 상태", v.State.request("CR-10").State, "반영")
	if !openTask(v.State, "PL-01 타 라인 배정안 검토") {
		t.Error("CR-10 후속 작업 없음")
	}
	v, err = reply("영업", "CR-11", `{"received":true,"orders":120}`)
	if err != nil {
		t.Fatalf("CR-11 회신 거절: %v", err)
	}
	wantS(t, "CR-11 상태", v.State.request("CR-11").State, "반영")
	if !openTask(v.State, "확정 납기 대응표(120건)") {
		t.Error("CR-11 후속 작업 없음")
	}
	// 계산은 그대로(기록만)
	after := v.State.Eval
	for _, p := range []string{ProductBasic, ProductDerma} {
		if before.Products[p].ScheduleTotal != after.Products[p].ScheduleTotal || before.Products[p].Months[0].Supply != after.Products[p].Months[0].Supply {
			t.Errorf("%s 계산이 바뀌었다: 제안 %d→%d", p, before.Products[p].ScheduleTotal, after.Products[p].ScheduleTotal)
		}
	}
	if v.State.Bid.State != BidAwaiting {
		t.Errorf("기록만 하는 회신 뒤 입찰 상태 %s(회신 대기 유지 기대)", v.State.Bid.State)
	}
}
