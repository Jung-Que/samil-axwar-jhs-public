package w05

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"axwar/server/store"
)

// 이슈 #28: E2 승인 입력자 권한, Masil 해제 상한(CR-08) = 기준일 보호량.

func TestDecisionAuthority(t *testing.T) {
	ctx := context.Background()
	b := loadBase(t)
	svc, err := NewService(ctx, b, store.NewMemory())
	if err != nil {
		t.Fatal(err)
	}
	rfp, _ := json.Marshal(RFPFromSource())
	if _, err := svc.Submit(ctx, EvRFP, "영업", rfp); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Submit(ctx, EvForApprove, "영업", []byte(`{"note":"상신"}`)); err != nil {
		t.Fatal(err)
	}
	dec := []byte(`{"participation":"조건부","options":{"OPT-02":"승인","OPT-01":"보류"},"conditions":["COND_PL01_UNCONFIRMED"]}`)
	for _, actor := range []string{"영업", "시스템", "에이전트", ""} {
		v, err := svc.Submit(ctx, EvDecision, actor, dec)
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("입력자 %q의 E2가 거절되지 않았다: %v", actor, err)
		}
		if n := len(v.State.Log); n != 2 {
			t.Errorf("입력자 %q 거절 뒤 사건 수 %d, 기대 2(기록 안 함)", actor, n)
		}
		if v.State.Bid.State != BidSubmitted || len(v.State.Decisions) != 0 {
			t.Errorf("입력자 %q 거절 뒤 상태 %s, 결정 %d건", actor, v.State.Bid.State, len(v.State.Decisions))
		}
	}
	stored, _ := svc.st.ListEvents(ctx)
	if len(stored) != 2 {
		t.Errorf("저장소 사건 %d개, 기대 2", len(stored))
	}
	v, err := svc.Submit(ctx, EvDecision, "CEO/이사회", dec)
	if err != nil {
		t.Fatalf("CEO/이사회 결정이 거절됨: %v", err)
	}
	if d := v.State.Decisions; len(d) != 1 || d[0].Approver != "CEO/이사회" {
		t.Errorf("결정 기록: %+v", d)
	}
}

func TestMasilReleaseCapByAsOf(t *testing.T) {
	ctx := context.Background()
	b := loadBase(t)
	// PL-02 Masil 발주: 06-22 20,000 · 06-23 24,000+20,000 · 06-26 24,000 → 06-25까지 64,000, 06-30까지 88,000
	for _, c := range []struct {
		asOf  string
		limit int64
	}{{"2026-06-25", 64_000}, {"2026-07-01", 88_000}} {
		svc, err := NewService(ctx, b, store.NewMemory())
		if err != nil {
			t.Fatal(err)
		}
		v, err := svc.RunScenario(ctx, "change", c.asOf)
		if err != nil {
			t.Fatalf("%s 변경 시나리오: %v", c.asOf, err)
		}
		r := v.State.request("CR-08")
		if r == nil || r.State != "요청" {
			t.Fatalf("%s CR-08이 열려 있지 않다: %+v", c.asOf, r)
		}
		wantInt(t, c.asOf+" CR-08 상한(max_qty)", r.MaxQty, c.limit)
		reply := func(q int64) (View, error) {
			return svc.Submit(ctx, EvReply, "Masil SCM", []byte(fmt.Sprintf(`{"request_id":"CR-08","answer":{"released_qty":%d}}`, q)))
		}
		if _, err := reply(c.limit + 1); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s 상한 %d + 1 회신이 거절되지 않았다: %v", c.asOf, c.limit, err)
		}
		v, err = reply(c.limit)
		if err != nil {
			t.Fatalf("%s 상한 %d 회신이 거절됨: %v", c.asOf, c.limit, err)
		}
		wantInt(t, c.asOf+" 반영된 해제 수량", v.State.Scenario.MasilReleased, c.limit)
	}
}
