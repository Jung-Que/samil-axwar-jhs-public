package w05

import (
	"math"
	"testing"
)

// 명세 §9 엔진 검증 기대값. 값이 바뀌면 명세와 재현 스크립트(w05_facts.py)를 먼저 고친다.

func loadBase(t *testing.T) *Base {
	t.Helper()
	b, err := LoadBase()
	if err != nil {
		t.Fatalf("LoadBase: %v", err)
	}
	return b
}

func eok(krw float64, nd int) float64 {
	p := math.Pow(10, float64(nd))
	return math.Round(krw/1e8*p) / p
}

func round(v float64, nd int) float64 {
	p := math.Pow(10, float64(nd))
	return math.Round(v*p) / p
}

func wantInt(t *testing.T, name string, got, want int64) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %d, 기대 %d", name, got, want)
	}
}

func wantF(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s = %v, 기대 %v", name, got, want)
	}
}

func wantS(t *testing.T, name, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %q, 기대 %q", name, got, want)
	}
}

func lineEval(ev *Evaluation, id string) LineEval {
	for _, l := range ev.Lines {
		if l.LineID == id {
			return l
		}
	}
	return LineEval{}
}

func optEval(ev *Evaluation, id string) OptionEval {
	for _, o := range ev.Options {
		if o.OptionID == id {
			return o
		}
	}
	return OptionEval{}
}

func TestManifest(t *testing.T) {
	n, err := VerifyManifest()
	if err != nil {
		t.Fatal(err)
	}
	if n != 15 {
		t.Errorf("원본 파일 %d개, 기대 15개", n)
	}
}

func TestOracleCapacityAndMasil(t *testing.T) {
	b := loadBase(t)
	ev := Evaluate(b, DefaultScenario())
	l1, l2 := lineEval(ev, LineBasic), lineEval(ev, LineDerma)
	wantInt(t, "L-01 기본 여유", l1.SpareAnnual, 201_755)
	wantInt(t, "L-02 기본 여유", l2.SpareAnnual, 58_484)
	wantF(t, "F1 가중 가동률", round(ev.F1WeightedUtil, 2), 88.38)
	wantInt(t, "Masil PL-01", l1.MasilProtected, 48_000)
	wantInt(t, "Masil PL-02", l2.MasilProtected, 88_000)
	wantInt(t, "C-201 기본 가용 PL-01", l1.BaseAvail, 153_755)
	wantInt(t, "C-201 기본 가용 PL-02", l2.BaseAvail, 0)
	wantInt(t, "Masil 부족 L-02", l2.MasilDeficit, 29_516)
	wantInt(t, "기본 월 공급 PL-01", l1.BaseMonthly, 12_812)
	wantInt(t, "기본 월 공급 PL-02", l2.BaseMonthly, 0)
	wantInt(t, "진행 중 B2B 건수", int64(ev.Commitments.InProgressCount), 1_146)
	wantInt(t, "진행 중 B2B 수량", ev.Commitments.InProgressQty, 6_659_055)
	wantInt(t, "PL-02 7월 추정 납품", ev.Commitments.EstDue[ProductDerma]["2026-07"], 430_547)
	wantInt(t, "PL-02 8월 추정 납품", ev.Commitments.EstDue[ProductDerma]["2026-08"], 434_222)
}

func TestOracleQualifiedDates(t *testing.T) {
	b := loadBase(t)
	for _, c := range []struct {
		val                     string
		o2Inst, o2Qual, o2Start string
		o1Inst, o1Qual, o1Start string
	}{
		{"A", "2026-08-24", "2026-09-21", "2026-10", "2027-03-15", "2027-04-12", "2027-05"},
		{"B", "2026-08-24", "2026-11-16", "2026-12", "2027-03-15", "2027-06-07", "2027-07"},
	} {
		sc := DefaultScenario()
		sc.Validation = c.val
		ev := Evaluate(b, sc)
		o2, o1 := optEval(ev, "OPT-02"), optEval(ev, "OPT-01")
		wantS(t, c.val+" OPT-02 설치", o2.InstallDate, c.o2Inst)
		wantS(t, c.val+" OPT-02 적격", o2.QualifiedDate, c.o2Qual)
		wantS(t, c.val+" OPT-02 산입", o2.StartMonth, c.o2Start)
		wantS(t, c.val+" OPT-01 설치", o1.InstallDate, c.o1Inst)
		wantS(t, c.val+" OPT-01 적격", o1.QualifiedDate, c.o1Qual)
		wantS(t, c.val+" OPT-01 산입", o1.StartMonth, c.o1Start)
		wantInt(t, "OPT-02 월 산입", o2.Monthly, 50_000)
		wantInt(t, "OPT-01 월 산입", o1.Monthly, 133_333)
	}
}

func TestOracleEconomics(t *testing.T) {
	b := loadBase(t)
	ev := Evaluate(b, DefaultScenario())
	p1, p2 := ev.Products[ProductBasic], ev.Products[ProductDerma]
	wantF(t, "PL-01 원화 단가", round(p1.UnitKRW, 3), 17_443.822)
	wantF(t, "PL-02 원화 단가", round(p2.UnitKRW, 3), 23_356.982)
	wantF(t, "PL-01 월 요구 금액(억)", eok(float64(p1.MonthlyDemand)*p1.UnitKRW, 2), 87.22)
	wantF(t, "PL-02 월 요구 금액(억)", eok(float64(p2.MonthlyDemand)*p2.UnitKRW, 2), 29.20)
	wantF(t, "연매출 기본", eok(ev.RevenueAt["0"], 1), 1_397.0)
	wantF(t, "연매출 -5%", eok(ev.RevenueAt["-5"], 1), 1_327.1)
	wantF(t, "연매출 -10%", eok(ev.RevenueAt["-10"], 1), 1_257.3)
	wantF(t, "공헌이익 기본", eok(ev.ContribAt["0"], 1), 974.9)
	wantF(t, "공헌이익 -5%", eok(ev.ContribAt["-5"], 1), 905.1)
	wantF(t, "공헌이익 -10%", eok(ev.ContribAt["-10"], 1), 835.2)
	wantF(t, "환율 100원 하락 매출", eok(ev.FXDown100Rev, 1), 94.5)
	wantF(t, "환율 100원 하락 공헌이익", eok(ev.FXDown100Contr, 1), 65.8)
	wantF(t, "계약 총액 기준 페널티(월)", eok(ev.PenaltyMonthly, 2), 41.91)
	wantF(t, "OPT-01 회수기간", round(optEval(ev, "OPT-01").PaybackMonths, 1), 11.3)
	wantF(t, "OPT-02 회수기간", round(optEval(ev, "OPT-02").PaybackMonths, 1), 1.9)
	wantF(t, "PL-01 원료비/EA", math.Round(p1.MaterialPerEA), 4_370)
	wantF(t, "PL-02 원료비/EA", math.Round(p2.MaterialPerEA), 10_659)
}

func TestOracleShortfallAndPenalty(t *testing.T) {
	b := loadBase(t)
	ev := Evaluate(b, DefaultScenario())
	p1 := ev.Products[ProductBasic]
	wantInt(t, "PL-01 전량 약속 월 부족", p1.Months[0].Short, 487_188)
	wantF(t, "PL-01 1개월 지연당(억)", eok(p1.PerLotMonth, 2), 2.55)
	wantF(t, "PL-01 12개월 노출(억)", eok(p1.PenaltyBacklog, 2), 198.86)

	cases := []struct {
		start, val string
		opt01      bool
		want       float64
	}{
		{"2026-10", "A", false, 40.99}, {"2026-10", "A", true, 26.98},
		{"2026-10", "B", false, 49.05}, {"2026-10", "B", true, 43.44},
		{"2027-01", "A", true, 9.63}, {"2027-01", "B", true, 21.37},
	}
	for _, c := range cases {
		sc := DefaultScenario()
		sc.DeliveryStart, sc.Validation = c.start, c.val
		if !c.opt01 {
			sc.Options["OPT-01"] = OptionDecision{Status: OptRejected}
		}
		got := eok(Evaluate(b, sc).Products[ProductDerma].PenaltyBacklog, 2)
		if got != c.want {
			t.Errorf("PL-02 12개월 노출 start=%s val=%s OPT-01=%v: %v억, 기대 %v억", c.start, c.val, c.opt01, got, c.want)
		}
	}

	sc := DefaultScenario()
	sc.Options["OPT-01"] = OptionDecision{Status: OptRejected}
	p2 := Evaluate(b, sc).Products[ProductDerma]
	wantInt(t, "PL-02 OPT-02만 월 부족", p2.Months[0].Short, 75_000)
	wantF(t, "PL-02 1개월 지연당(억)", eok(p2.PerLotMonth, 2), 0.53)

	sc.MasilInUtil = true
	wantInt(t, "Masil 포함 대안 월 부족", Evaluate(b, sc).Products[ProductDerma].Months[0].Short, 70_127)
}
