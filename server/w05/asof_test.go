package w05

import (
	"context"
	"errors"
	"strings"
	"testing"

	"axwar/server/store"
)

// 기준일 재현(플랜 06_기준일_재현_플랜.md) 검사: §2 표의 기준일별 값, 누수 금지, 기준일 상대 시나리오.

func TestAsOfTable(t *testing.T) {
	b := loadBase(t)
	cases := []struct {
		date                    string
		open                    int
		openQty, pl01, pl02     int64
		last7, last30, shipped7 int
	}{
		{"2024-06-30", 1201, 8_171_220, 2_809_282, 1_012_258, 103, 600, 141},
		{"2024-12-31", 1155, 7_791_036, 2_609_645, 973_335, 45, 631, 115},
		{"2025-06-30", 1168, 7_817_583, 2_679_235, 1_021_489, 66, 626, 135},
		{"2025-12-31", 1145, 7_703_334, 2_586_588, 990_776, 47, 601, 138},
		{"2026-03-31", 1200, 8_155_796, 2_734_044, 1_043_120, 56, 619, 122},
		{"2026-06-30", 1146, 6_659_055, 2_240_220, 864_769, 78, 629, 155},
	}
	for _, c := range cases {
		a := b.At(c.date)
		cm := a.Commit
		wantInt(t, c.date+" 진행 B2B 건수", int64(cm.InProgressCount), int64(c.open))
		wantInt(t, c.date+" 진행 B2B 수량", cm.InProgressQty, c.openQty)
		wantInt(t, c.date+" PL-01 진행 수량", cm.ByProduct[ProductBasic], c.pl01)
		wantInt(t, c.date+" PL-02 진행 수량", cm.ByProduct[ProductDerma], c.pl02)
		s := &State{Scenario: DefaultScenario()}
		s.Scenario.SimDate = c.date
		in := s.Inbox(b, 7, 10)
		wantInt(t, c.date+" 7일 접수", int64(in.Last7), int64(c.last7))
		wantInt(t, c.date+" 30일 접수", int64(in.Last30), int64(c.last30))
		wantInt(t, c.date+" 7일 출하", int64(in.Shipped7), int64(c.shipped7))
		if len(in.Orders) != 10 || !in.Truncated {
			t.Errorf("%s 접수함 limit 10: %d건, 잘림 %v", c.date, len(in.Orders), in.Truncated)
		}
	}
}

// 판단에 쓰는 값이 모두 잘라 낸 날까지의 기록에서만 나오는지 본다.
func TestAsOfNoLeakage(t *testing.T) {
	b := loadBase(t)
	for _, d := range []string{"2024-07-01", "2025-03-14", "2025-12-31", "2026-06-30", "2026-07-01"} {
		a := b.At(d)
		c := a.Cutoff
		if c > d || c > DataEnd {
			t.Errorf("%s 잘라 낸 날 %s", d, c)
		}
		if a.FXDate == "" || a.FXDate > c {
			t.Errorf("%s 환율 날짜 %q가 잘라 낸 날 %s 뒤", d, a.FXDate, c)
		}
		for p, mc := range a.MatCost {
			if mc.LastMonth > c[:7] {
				t.Errorf("%s %s 원가 기준월 %s가 %s 뒤", d, p, mc.LastMonth, c)
			}
		}
		shipped := 0
		for _, o := range b.Orders {
			if o.ShipDate != "" && o.ShipDate <= c {
				shipped++
			}
		}
		lt := 0
		for _, v := range a.LeadTimes {
			lt += len(v)
		}
		if lt != shipped {
			t.Errorf("%s 리드타임 표본 %d건 ≠ 잘라 낸 날까지 출하 %d건(뒤 출하가 섞임)", d, lt, shipped)
		}
		for _, s := range a.SPOs {
			if s.OrderDate > c {
				t.Errorf("%s Masil 발주 %s(%s)가 잘라 낸 날 뒤", d, s.POID, s.OrderDate)
			}
		}
		for _, o := range b.OpenOrders(c) {
			if o.OrderDate > c || (o.ShipDate != "" && o.ShipDate <= c) {
				t.Errorf("%s 진행 주문 %s 판정 오류", d, o.OrderID)
				break
			}
		}
		if a.FutureSnapshot != (c < DataEnd) {
			t.Errorf("%s 미래 스냅샷 표시 %v", d, a.FutureSnapshot)
		}
	}
}

func newSvc(t *testing.T) *Service {
	t.Helper()
	svc, err := NewService(context.Background(), loadBase(t), store.NewMemory())
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

// 다른 기준일에서 같은 시연이 같은 규칙으로 돈다(숫자는 날짜에 따라 다름).
func TestSessionScenarios(t *testing.T) {
	svc := newSvc(t)
	ctx := context.Background()
	v, err := svc.RunScenario(ctx, "normal", "2025-12-31")
	if err != nil {
		t.Fatal(err)
	}
	s := v.State
	wantS(t, "세션 기준일", s.Scenario.AsOf, "2025-12-31")
	wantS(t, "이사회", s.Scenario.BoardDate, "2026-01-05")
	wantS(t, "납품 개시 가정", s.Scenario.DeliveryStart, "2026-03")
	wantS(t, "입찰 상태", s.Bid.State, BidAwaiting)
	wantS(t, "평가 기록 마지막 날", s.Eval.Cutoff, "2025-12-31")
	if !s.Eval.FutureSnapshot || !hasFlag(s.Eval, "FUTURE_SNAPSHOT") {
		t.Error("2025-12-31 세션에 미래 스냅샷 표시 없음")
	}
	if s.Eval.FX == 0 || s.Eval.FXDate > "2025-12-31" {
		t.Errorf("환율 %v(%s)", s.Eval.FX, s.Eval.FXDate)
	}
	wantS(t, "PL-02 첫 달", line(s, ProductDerma).Schedule[0].Month, "2026-03")
	wantInt(t, "Masil 보호(발주 전)", lineEval(s.Eval, LineDerma).MasilProtected, 0)
	if len(v.Lanes) != len(LaneRoles) || len(v.Lines) != 7 { // 6개 라인 + 승인된 F2 신규 더마 라인
		t.Errorf("레인 %d개, 라인 %d개", len(v.Lanes), len(v.Lines))
	}

	v, err = svc.RunScenario(ctx, "reject", "2025-12-31")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"CR-01", "CR-02", "CR-03"} {
		if r := v.State.request(id); id != "CR-03" && (r == nil || r.State != "기한 초과") {
			t.Errorf("%s 기한 초과 아님: %+v", id, r)
		}
	}
	if v.State.request("CR-03") != nil {
		t.Error("Masil 발주 전 기준일인데 CR-03(Masil 포함 여부)이 생김")
	}

	if _, err := svc.Reset(ctx, "2024-06-28"); !errors.Is(err, ErrInvalid) {
		t.Errorf("범위 밖 기준일이 통과: %v", err)
	}
	if _, err := svc.Submit(ctx, EvSession, "시스템", []byte(`{"as_of":"2025-01-02"}`)); err == nil {
		t.Error("사건이 있는데 세션 기준일을 다시 정함")
	}
}

func TestBacktestAndBoards(t *testing.T) {
	svc := newSvc(t)
	ctx := context.Background()
	if _, err := svc.Reset(ctx, "2025-12-31"); err != nil {
		t.Fatal(err)
	}
	bt, err := svc.Backtest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !bt.Available || bt.Orders[ProductDerma] != 228 || len(bt.Rows[ProductDerma]) == 0 {
		t.Fatalf("정답 비교: %+v", bt)
	}
	if h := bt.HitRate[ProductDerma]; h <= 0 || h > 100 {
		t.Errorf("PL-02 적중률 %v", h)
	}
	if _, err := svc.Reset(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if bt, _ := svc.Backtest(ctx); bt.Available {
		t.Error("자료 마지막 날 기준인데 정답 비교가 열림")
	}
	v, _ := svc.Snapshot(ctx)
	var prod Lane
	for _, l := range v.Lanes {
		if l.Role == "생산관리" {
			prod = l
		}
	}
	if len(prod.Doing) < 6 || !strings.Contains(prod.Doing[0].Text, "배치") {
		t.Errorf("생산관리 레인 하고 있는 일: %+v", prod.Doing)
	}
	for _, r := range v.Lines {
		if r.LineID == LineDerma && (r.BatchesTypical != 3 || r.OpenOrders != 228 || r.T12Monthly != 477_621) {
			t.Errorf("L-02 라인 보드: %+v", r)
		}
	}
}

func hasFlag(ev *Evaluation, code string) bool {
	for _, f := range ev.Flags {
		if f.Code == code {
			return true
		}
	}
	return false
}
