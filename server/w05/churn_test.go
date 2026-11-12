package w05

import (
	"context"
	"errors"
	"math"
	"testing"

	"axwar/server/store"
)

// 이슈 #33: R12 이탈 경보·R13 반환 금지. 기대값은 원본에서 따로 재현했다
// (01_도메인_지식.md §9 R12 백테스트: C-101 단월 4월 0.804·5월 0.691·6월 0.665, 6월 3개월 0.722·YoY −23.1%).

func churnOf(sigs []ChurnSignal, id string) *ChurnSignal {
	for i := range sigs {
		if sigs[i].CustomerID == id {
			return &sigs[i]
		}
	}
	return nil
}

func near(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %.4f, 기대 %.4f", name, got, want)
	}
}

func TestChurnSignalAsOf(t *testing.T) {
	b := loadBase(t)
	a := b.At(DefaultAsOf)
	if len(a.Churn) != 2 {
		t.Fatalf("대상 고객(매출 비중 10%% 이상) %d곳, 기대 2(C-101·C-102)", len(a.Churn))
	}
	c := churnOf(a.Churn, "C-101")
	wantS(t, "C-101 판단 달", c.Month, "2026-06")
	wantS(t, "C-101 상태", c.Status, ChurnAtRisk)
	wantS(t, "C-101 경보 시작", c.Since, "2026-05")
	near(t, "C-101 3개월 수주/출하", c.Rolling, 0.722, 0.0005)
	near(t, "C-101 3개월 수주 YoY", c.YoY, -23.1, 0.05)
	want := map[string]struct {
		single float64
		status string
	}{"2026-04": {0.804, ChurnOK}, "2026-05": {0.691, ChurnWatch}, "2026-06": {0.665, ChurnAtRisk}}
	for _, h := range c.History {
		if w, ok := want[h.Month]; ok {
			near(t, "C-101 "+h.Month+" 단월", h.Single, w.single, 0.0005)
			wantS(t, "C-101 "+h.Month+" 상태", h.Status, w.status)
		}
	}
	if len(c.History) != 6 {
		t.Errorf("C-101 이력 %d개월, 기대 6", len(c.History))
	}
	wantS(t, "C-102 상태", churnOf(a.Churn, "C-102").Status, ChurnOK)
	if c.OpenOrders == 0 || c.T12BookedQty[ProductBasic] == 0 || c.T12BookedQty[ProductDerma] == 0 {
		t.Errorf("C-101 진행 주문·12개월 수주: %d건, %v", c.OpenOrders, c.T12BookedQty)
	}

	// 기준일 2025-12-31: C-101은 정상(3개월 0.989, YoY +4.6%)
	e := churnOf(b.At("2025-12-31").Churn, "C-101")
	wantS(t, "2025-12-31 C-101 상태", e.Status, ChurnOK)
	near(t, "2025-12-31 C-101 3개월", e.Rolling, 0.989, 0.0005)
	// 기록이 15개월이 안 되는 기준일에는 YoY를 재지 않는다(AT_RISK 불가)
	if x := churnOf(b.At("2024-12-31").Churn, "C-101"); x.HasYoY || x.Status == ChurnAtRisk {
		t.Errorf("2024-12-31 C-101: YoY %v, 상태 %s", x.HasYoY, x.Status)
	}
}

func TestChurnNoReturnUntilConfirmed(t *testing.T) {
	ctx := context.Background()
	b := loadBase(t)

	// 2025-12-31 세션: 경보 없음 → CR-09 없음
	svc, _ := NewService(ctx, b, store.NewMemory())
	v, err := svc.RunScenario(ctx, "normal", "2025-12-31")
	if err != nil {
		t.Fatal(err)
	}
	if v.State.request("CR-09") != nil {
		t.Error("2025-12-31에 CR-09가 생겼다")
	}

	// 2026-07-01 세션: CR-09 열림, 회신 전 반환 0
	svc, _ = NewService(ctx, b, store.NewMemory())
	v, err = svc.RunScenario(ctx, "normal", "")
	if err != nil {
		t.Fatal(err)
	}
	if v.State.request("CR-09") == nil {
		t.Fatal("CR-09가 없다")
	}
	sumRel := func(ev *Evaluation) (q int64) {
		for _, p := range []string{ProductBasic, ProductDerma} {
			for _, m := range ev.Products[p].Months {
				q += supplyOf(m, "CUST-REL")
			}
		}
		return
	}
	if q := sumRel(v.State.Eval); q != 0 {
		t.Errorf("회신 전 C-201 반환 %d, 기대 0", q)
	}
	hasFlag := func(ev *Evaluation, code string) bool {
		for _, f := range ev.Flags {
			if f.Code == code {
				return true
			}
		}
		return false
	}
	if !hasFlag(v.State.Eval, "CHURN_C-101") || !hasFlag(v.State.Eval, "R13_NO_RETURN") {
		t.Error("이탈 경보·반환 금지 플래그가 없다")
	}

	reply := func(actor, answer string) (View, error) {
		return svc.Submit(ctx, EvReply, actor, []byte(`{"request_id":"CR-09","answer":`+answer+`}`))
	}
	ok := `{"revised":true,"release_pl01":120000,"release_pl02":24000}`
	for _, actor := range []string{"영업", "생산관리", "CEO/이사회"} {
		if _, err := reply(actor, ok); !errors.Is(err, ErrInvalid) {
			t.Errorf("입력자 %q의 CR-09 회신이 거절되지 않았다: %v", actor, err)
		}
	}
	if _, err := reply("영업 임원", `{"revised":false,"release_pl01":1}`); !errors.Is(err, ErrInvalid) {
		t.Errorf("수정 아님인데 반환 수량이 있는 회신이 통과: %v", err)
	}
	if _, err := reply("영업 임원", `{"revised":true,"release_pl02":99999999}`); !errors.Is(err, ErrInvalid) {
		t.Errorf("12개월 수주를 넘는 반환이 통과: %v", err)
	}
	v, err = reply("영업 임원", ok)
	if err != nil {
		t.Fatalf("영업 임원 회신 거절: %v", err)
	}
	wantInt(t, "반영 뒤 PL-01 월 반환", supplyOf(v.State.Eval.Products[ProductBasic].Months[0], "CUST-REL"), 10_000)
	wantInt(t, "반영 뒤 PL-02 월 반환", supplyOf(v.State.Eval.Products[ProductDerma].Months[0], "CUST-REL"), 2_000)
	if hasFlag(v.State.Eval, "R13_NO_RETURN") {
		t.Error("수정 확인 뒤에도 반환 금지 플래그가 남아 있다")
	}
}
