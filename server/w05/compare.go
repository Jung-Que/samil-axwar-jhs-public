package w05

// OPT-01 비교 시나리오(이슈 #29). 표시용이며 판단·제출·결정 결속에는 쓰지 않는다.
//
// 기본(11/15 제출 기준): 두 증설안 모두 설치 뒤 적격 기간(가정 A-08, A 4주 / B 12주 또는 회신 주수)을 더한다.
// 비교: 원본 strat_capex_plan_cv의 OPT-01 노트 "원료 재계약·처방 재검증 불필요"를 적용해 OPT-01 적격 기간을 0주로 둔다.
// 설치 완료일(결정일 + 36주) 다음 달부터 산입한다. 다른 입력(결정·회신·개시월·OPT-02)은 기본과 같다.

const opt01Source = "strat_capex_plan_cv OPT-01 노트: \"원료 재계약·처방 재검증 불필요. 리드타임 약 9개월(36주)\""

// OptCompare는 기본과 비교 시나리오의 OPT-01 반영 시점과 PL-02 월별 공급 차이다.
type OptCompare struct {
	OptionID string         `json:"option_id"`
	Source   string         `json:"source"`
	Base     CompareSide    `json:"base"`
	Alt      CompareSide    `json:"alt"`
	Months   []CompareMonth `json:"months"`
}

// CompareSide는 한 시나리오의 OPT-01 일정과 PL-02 계약 기간 합계다.
type CompareSide struct {
	Label            string  `json:"label"`
	ValidationWeeks  int     `json:"validation_weeks"`
	Status           string  `json:"status"`
	InstallDate      string  `json:"install_date"`
	QualifiedDate    string  `json:"qualified_date"`
	StartMonth       string  `json:"start_month"`        // 산입 시작월(적격일 다음 달)
	FirstSupplyMonth string  `json:"first_supply_month"` // 계약 기간 안에서 OPT-01 물량이 처음 공급에 들어간 달("" = 없음)
	Usable           bool    `json:"usable"`
	Conditional      bool    `json:"conditional"`
	OptQtyTotal      int64   `json:"opt_qty_total"` // 계약 기간 OPT-01 공급 합(EA)
	ScheduleTotal    int64   `json:"schedule_total"`
	ShortTotal       int64   `json:"short_total"`
	CoveragePct      float64 `json:"coverage_pct"`
	PenaltyBacklog   float64 `json:"penalty_backlog_krw"`
	PenaltyContract  float64 `json:"penalty_contract_krw"`
}

// CompareMonth는 PL-02 한 달의 두 시나리오 값이다(EA/월).
type CompareMonth struct {
	Month        string `json:"month"`
	Demand       int64  `json:"demand"`
	BaseOpt01    int64  `json:"base_opt01"`
	AltOpt01     int64  `json:"alt_opt01"`
	BaseSupply   int64  `json:"base_supply"`
	AltSupply    int64  `json:"alt_supply"`
	BaseSchedule int64  `json:"base_schedule"`
	AltSchedule  int64  `json:"alt_schedule"`
	BaseShort    int64  `json:"base_short"`
	AltShort     int64  `json:"alt_short"`
}

func compareOPT01(base, alt *Evaluation) *OptCompare {
	c := &OptCompare{OptionID: "OPT-01", Source: opt01Source,
		Base: compareSide(base, "기본(제출 기준): 적격 기간 가정 A-08 적용"),
		Alt:  compareSide(alt, "비교: 원본 '재검증 불필요' 적용(적격 기간 0주)")}
	bm, am := base.Products[ProductDerma].Months, alt.Products[ProductDerma].Months
	for i := range bm {
		if i >= len(am) {
			break
		}
		x, y := bm[i], am[i]
		c.Months = append(c.Months, CompareMonth{Month: x.Month, Demand: x.Demand,
			BaseOpt01: supplyOf(x, "OPT-01"), AltOpt01: supplyOf(y, "OPT-01"),
			BaseSupply: x.Supply, AltSupply: y.Supply, BaseSchedule: x.Schedule, AltSchedule: y.Schedule, BaseShort: x.Short, AltShort: y.Short})
	}
	return c
}

func compareSide(ev *Evaluation, label string) CompareSide {
	s := CompareSide{Label: label, PenaltyContract: ev.PenaltyContract}
	for _, oe := range ev.Options {
		if oe.OptionID == "OPT-01" {
			s.ValidationWeeks, s.Status, s.InstallDate, s.QualifiedDate, s.StartMonth = oe.ValidationWeeks, oe.Status, oe.InstallDate, oe.QualifiedDate, oe.StartMonth
			s.Usable, s.Conditional = oe.Usable, oe.Conditional
		}
	}
	pe := ev.Products[ProductDerma]
	s.ScheduleTotal, s.ShortTotal, s.CoveragePct, s.PenaltyBacklog = pe.ScheduleTotal, pe.ShortTotal, pe.CoveragePct, pe.PenaltyBacklog
	for _, m := range pe.Months {
		if q := supplyOf(m, "OPT-01"); q > 0 {
			if s.FirstSupplyMonth == "" {
				s.FirstSupplyMonth = m.Month
			}
			s.OptQtyTotal += q
		}
	}
	return s
}

func supplyOf(m MonthRow, basis string) int64 {
	var q int64
	for _, s := range m.Supplies {
		if s.Basis == basis {
			q += s.Qty
		}
	}
	return q
}
