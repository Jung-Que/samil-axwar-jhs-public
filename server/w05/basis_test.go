package w05

import (
	"math"
	"strings"
	"testing"
)

// 이슈 #27: 수치 출처·기간, 원본 간 규모 차이, 빠진 가정 공개. 계산값은 바꾸지 않는다(기존 기대값 테스트가 그대로 지킨다).

func TestDataBasisAndGaps(t *testing.T) {
	b := loadBase(t)
	s := &State{Scenario: DefaultScenario()}
	d := s.Basis(b)
	if d.SnapshotFile != FileCapacity || d.SnapshotDate != "2026-06-30" || d.BatchFile != FileBatches {
		t.Errorf("출처: %+v", d)
	}
	if d.T12From != "2025-07" || d.T12To != "2026-06" {
		t.Errorf("배치 실적 집계 기간 %s~%s, 기대 2025-07~2026-06", d.T12From, d.T12To)
	}
	wantInt(t, "대리점 주문 제외 건수", int64(d.AgencyOrders), 601)
	wantInt(t, "대리점 주문 제외 수량", d.AgencyQty, 213_865)
	want := map[string]struct {
		t12       int64
		annualPct float64
		monthPct  float64
	}{
		"L-01": {1_293_334, 1153.9, 96.2},
		"L-02": {477_621, 294.0, 24.5},
	}
	seen := 0
	for _, g := range d.Gaps {
		w, ok := want[g.LineID]
		if !ok {
			continue
		}
		seen++
		wantInt(t, g.LineID+" 배치 실적 월 생산", g.T12Monthly, w.t12)
		if math.Abs(g.RatioAnnual-w.annualPct) > 0.1 || math.Abs(g.RatioMonthly-w.monthPct) > 0.1 {
			t.Errorf("%s 규모 비율: 연간 해석 %.1f%%(기대 %.1f) · 월간 해석 %.1f%%(기대 %.1f)", g.LineID, g.RatioAnnual, w.annualPct, g.RatioMonthly, w.monthPct)
		}
	}
	if seen != 2 || len(d.Gaps) != 6 {
		t.Errorf("규모 차이 행: %d개(기존 라인 6개 기대)", len(d.Gaps))
	}

	// 대리점 주문은 2024-07-01부터 있다. 그 전 기준일에서는 0건
	early := &State{Scenario: SessionScenario("2024-07-01")}
	early.Scenario.SimDate = "2024-06-30"
	if n, q := b.AgencyOpen(early.Scenario.SimDate); n != 0 || q != 0 {
		t.Errorf("2024-06-30 대리점 제외 %d건 %d EA, 기대 0", n, q)
	}
}

func TestAssumptionsDisclosed(t *testing.T) {
	b := loadBase(t)
	as := assumptions(b, DefaultScenario())
	byID := map[string]Assumption{}
	for _, a := range as {
		if a.Affects == "" {
			t.Errorf("%s: 영향(쓰는 곳)이 비어 있다", a.ID)
		}
		byID[a.ID] = a
	}
	for _, id := range []string{"A-16", "A-17", "A-18", "A-19", "A-20", "A-21", "A-22"} {
		if _, ok := byID[id]; !ok {
			t.Errorf("가정 %s가 표에 없다", id)
		}
	}
	if v := byID["A-17"].Value; !strings.Contains(v, "601건") || !strings.Contains(v, "213,865 EA") || !strings.Contains(v, "약 21만") {
		t.Errorf("A-17 대리점 제외 값: %q", v)
	}
	if v := byID["A-19"].Value; !strings.Contains(v, "48,000") || !strings.Contains(v, "88,000") {
		t.Errorf("A-19 Masil 보호 값: %q", v)
	}
	if a := byID["A-22"]; !strings.Contains(a.Value, "2026-07-06(월)") || a.Label != "원본" {
		t.Errorf("A-22 기본 기준일 이사회: %+v", a)
	}
	// 다른 기준일에서 이사회 +5일은 빌려 쓴 가정이고, 주말이면 그대로 보인다
	sc := SessionScenario("2026-03-31")
	if a := assumptionByID(assumptions(b, sc), "A-22"); !strings.Contains(a.Value, "2026-04-05(일)") || a.Label != "가정" {
		t.Errorf("A-22 기준일 2026-03-31: %+v", a)
	}
}

func assumptionByID(as []Assumption, id string) Assumption {
	for _, a := range as {
		if a.ID == id {
			return a
		}
	}
	return Assumption{}
}
