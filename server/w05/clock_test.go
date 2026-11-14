package w05

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"axwar/server/store"
)

// 이슈 #41: 원본 기록으로 굴러가는 시계·신규 수주 반영. 기대값은 파이썬으로 따로 재현했다
// (진행 주문 순증분 2024-12-31 → 2025-06-30: PL-01 +69,590 · PL-02 +48,154, 2026-03-31 → 06-30: 둘 다 감소).

func logText(st *State) string {
	var all []string
	for _, e := range st.Log {
		all = append(all, e.Changes...)
	}
	return strings.Join(all, "\n")
}

func clockTo(t *testing.T, svc *Service, sid, day string) View {
	t.Helper()
	v, err := svc.SubmitIn(context.Background(), sid, EvClock, "시스템", []byte(`{"sim_date":"`+day+`"}`))
	if err != nil {
		t.Fatalf("시계 %s: %v", day, err)
	}
	return v
}

func lineOf(ev *Evaluation, p string) LineEval {
	for _, l := range ev.Lines {
		if l.ProductID == p {
			return l
		}
	}
	return LineEval{}
}

func TestHistoryClockAppliesNetNewOrders(t *testing.T) {
	ctx := context.Background()
	b := loadBase(t)
	svc, _ := NewService(ctx, b, store.NewMemory())
	v, err := svc.NewSession(ctx, "시계 시험", "2024-12-31")
	if err != nil {
		t.Fatal(err)
	}
	sid := v.Session.ID
	before := lineOf(v.State.Eval, ProductDerma)
	v = clockTo(t, svc, sid, "2025-06-30")
	h := v.State.Scenario.HistOrders
	wantInt(t, "PL-01 순증", h[ProductBasic], 69_590)
	wantInt(t, "PL-02 순증", h[ProductDerma], 48_154)
	l2 := lineOf(v.State.Eval, ProductDerma)
	wantInt(t, "L-02 기준일 뒤 신규 수주", l2.HistOrders, 48_154)
	wantInt(t, "L-02 C-201 가용", l2.BaseAvail, before.BaseAvail-48_154)
	txt := logText(v.State)
	if !strings.Contains(txt, "원본 기록(2024-12-31 다음 날 ~ 2025-06-30)") || !strings.Contains(txt, "기준일 뒤 신규 수주 반영") {
		t.Errorf("시계 로그에 원본 기록·신규 수주 반영이 없다:\n%s", txt)
	}
	found := false
	for _, f := range v.State.Eval.Flags {
		if f.Code == "HIST_ORDERS" {
			found = true
		}
	}
	if !found {
		t.Error("HIST_ORDERS 플래그 없음")
	}

	// 모의 가정으로 총량 기준을 고르면 한 기간 신규 수주 전체를 뺀다(가동률 이중 차감이라 기본이 아님)
	v, err = svc.SubmitIn(ctx, sid, EvSimAssumption, "시뮬레이터", []byte(`{"new_order_basis":"gross"}`))
	if err != nil {
		t.Fatal(err)
	}
	w := b.histWindow("2024-12-31", "2025-06-30")
	wantInt(t, "총량 기준 PL-02", v.State.Scenario.HistOrders[ProductDerma], w.NewQty[ProductDerma])
	if lineOf(v.State.Eval, ProductDerma).BaseAvail != 0 {
		t.Errorf("총량 기준이면 L-02 가용 0 기대: %d", lineOf(v.State.Eval, ProductDerma).BaseAvail)
	}
}

func TestHistoryClockChurnAndCR09(t *testing.T) {
	ctx := context.Background()
	b := loadBase(t)
	svc, _ := NewService(ctx, b, store.NewMemory())
	v, err := svc.NewSession(ctx, "C-101 경보 시험", "2026-03-31")
	if err != nil {
		t.Fatal(err)
	}
	sid := v.Session.ID
	rfp, _ := json.Marshal(RFPFromSource())
	if v, err = svc.SubmitIn(ctx, sid, EvRFP, "영업", rfp); err != nil {
		t.Fatal(err)
	}
	if v.State.request("CR-09") != nil {
		t.Fatal("2026-03-31 접수 때 CR-09가 있으면 안 된다(C-101 정상)")
	}
	v = clockTo(t, svc, sid, "2026-06-30")
	if len(v.State.Scenario.HistOrders) != 0 {
		t.Errorf("진행 주문이 줄어든 기간인데 신규 수주를 뺐다: %v", v.State.Scenario.HistOrders)
	}
	txt := logText(v.State)
	if !strings.Contains(txt, "이탈 신호 C-101: 정상 → AT_RISK") {
		t.Errorf("이탈 신호 변화가 로그에 없다:\n%s", txt)
	}
	if r := v.State.request("CR-09"); r == nil || r.ToRole != "영업 임원" {
		t.Errorf("시계로 켜진 이탈 경보에 CR-09가 생기지 않았다: %+v", r)
	}
}

func TestHistoryClockNoChangeAfterDataEnd(t *testing.T) {
	ctx := context.Background()
	b := loadBase(t)
	svc, _ := NewService(ctx, b, store.NewMemory())
	v := clockTo(t, svc, store.MainSession, "2026-07-20")
	if len(v.State.Scenario.HistOrders) != 0 || !strings.Contains(logText(v.State), "원본 기록은 2026-06-30까지") {
		t.Errorf("데이터 끝 뒤 시계: %v / %s", v.State.Scenario.HistOrders, logText(v.State))
	}
}
