package w05

import "testing"

// 이슈 #29: OPT-01 비교 시나리오(원본 '재검증 불필요'). 기본 시나리오 값은 그대로다(engine_test 기대값이 지킨다).

func TestCompareOPT01(t *testing.T) {
	b := loadBase(t)
	ev := Evaluate(b, DefaultScenario())
	c := ev.CompareOPT01
	if c == nil {
		t.Fatal("비교 시나리오가 없다")
	}
	// 기본: 설치 2027-03-15 + 4주 → 적격 2027-04-12 → 산입 2027-05. 비교: 적격 = 설치일 → 산입 2027-04
	wantS(t, "기본 OPT-01 설치", c.Base.InstallDate, "2027-03-15")
	wantS(t, "기본 OPT-01 산입", c.Base.StartMonth, "2027-05")
	wantS(t, "비교 OPT-01 설치", c.Alt.InstallDate, "2027-03-15")
	wantS(t, "비교 OPT-01 적격", c.Alt.QualifiedDate, "2027-03-15")
	wantS(t, "비교 OPT-01 산입", c.Alt.StartMonth, "2027-04")
	wantS(t, "기본 최초 반영 월", c.Base.FirstSupplyMonth, "2027-05")
	wantS(t, "비교 최초 반영 월", c.Alt.FirstSupplyMonth, "2027-04")
	if c.Base.ValidationWeeks != 4 || c.Alt.ValidationWeeks != 0 {
		t.Errorf("적격 기간: 기본 %d주 · 비교 %d주", c.Base.ValidationWeeks, c.Alt.ValidationWeeks)
	}
	// 기본 평가 자체는 바뀌지 않는다
	for _, oe := range ev.Options {
		if oe.OptionID == "OPT-01" && (oe.StartMonth != "2027-05" || oe.ValidationWeeks != 4) {
			t.Errorf("기본 평가의 OPT-01이 바뀌었다: %s, %d주", oe.StartMonth, oe.ValidationWeeks)
		}
	}
	// 2027-04: 기본은 OPT-02 50,000만, 비교는 OPT-01 133,333이 더해져 요구 125,000을 채운다
	var apr *CompareMonth
	for i := range c.Months {
		if c.Months[i].Month == "2027-04" {
			apr = &c.Months[i]
		}
	}
	if apr == nil {
		t.Fatal("2027-04 행이 없다")
	}
	wantInt(t, "2027-04 기본 OPT-01", apr.BaseOpt01, 0)
	wantInt(t, "2027-04 비교 OPT-01", apr.AltOpt01, 133_333)
	wantInt(t, "2027-04 기본 공급", apr.BaseSupply, 50_000)
	wantInt(t, "2027-04 비교 공급", apr.AltSupply, 183_333)
	wantInt(t, "2027-04 기본 제안", apr.BaseSchedule, 50_000)
	wantInt(t, "2027-04 비교 제안", apr.AltSchedule, 125_000)
	wantInt(t, "계약 기간 제안 차이", c.Alt.ScheduleTotal-c.Base.ScheduleTotal, 75_000)
	if !(c.Alt.PenaltyBacklog < c.Base.PenaltyBacklog) {
		t.Errorf("비교 시나리오 지연분 노출 %.0f가 기본 %.0f보다 작아야 한다", c.Alt.PenaltyBacklog, c.Base.PenaltyBacklog)
	}

	// 적격 B(12주): 기본 산입 2027-07, 비교는 그대로 2027-04
	sc := DefaultScenario()
	sc.Validation = "B"
	cb := Evaluate(b, sc).CompareOPT01
	wantS(t, "B 기본 OPT-01 산입", cb.Base.StartMonth, "2027-07")
	wantS(t, "B 비교 OPT-01 산입", cb.Alt.StartMonth, "2027-04")
}

func TestCompareOPT01Rejected(t *testing.T) {
	s := runScenario(t, "reject") // OPT-01 거절
	c := s.Eval.CompareOPT01
	if c == nil {
		t.Fatal("비교 시나리오가 없다")
	}
	if c.Base.Usable || c.Alt.Usable || c.Base.FirstSupplyMonth != "" || c.Alt.FirstSupplyMonth != "" {
		t.Errorf("OPT-01 거절이면 두 시나리오 모두 반영 없음: 기본 %+v / 비교 %+v", c.Base, c.Alt)
	}
}
