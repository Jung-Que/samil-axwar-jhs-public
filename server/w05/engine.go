package w05

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// 옵션(증설·전환안) 결정 상태.
const (
	OptProposed = "제안"
	OptApproved = "승인"
	OptDeferred = "보류"
	OptRejected = "거절"
)

// RFP 품목(바이어 제시안). E1에서 구조화된 필드로 들어온다.
type RFPItem struct {
	ProductID    string  `json:"product_id"`
	AnnualQty    int64   `json:"annual_qty"`
	UnitPriceUSD float64 `json:"unit_price_usd"`
}

// OptionDecision은 승인권자가 옵션에 내린 결정이다.
type OptionDecision struct {
	Status       string `json:"status"`
	DecisionDate string `json:"decision_date,omitempty"`
}

// Scenario는 평가 입력이다. 가정(A-xx)과 회신으로 바뀐 값이 모두 여기에 모인다.
type Scenario struct {
	AsOf               string                    `json:"as_of"`      // 세션 기준일(E00_SESSION). 비면 DefaultAsOf
	BoardDate          string                    `json:"board_date"` // 이사회 = 기준일 + 5일
	SimDate            string                    `json:"sim_date"`
	Items              []RFPItem                 `json:"items"`
	PriceCut           float64                   `json:"price_cut"`       // 0, -0.07 …
	DeliveryStart      string                    `json:"delivery_start"`  // YYYY-MM (A-04)
	StartConfirmed     bool                      `json:"start_confirmed"` // 바이어 회신으로 확인
	ContractMonths     int                       `json:"contract_months"` // A-05
	Validation         string                    `json:"validation"`      // A|B (A-08)
	ValidationWeeks    int                       `json:"validation_weeks"`
	PenaltyBasis       string                    `json:"penalty_basis"` // backlog|contract (A-13)
	PenaltyConfirmed   bool                      `json:"penalty_confirmed"`
	MasilInUtil        bool                      `json:"masil_in_util"`   // A-07을 끄면 true
	CapacityPeriod     string                    `json:"capacity_period"` // annual|monthly (D6·R4)
	UnitConfirmed      bool                      `json:"unit_confirmed"`
	PL01Confirmed      bool                      `json:"pl01_confirmed"`
	PL01ExtraAnnual    int64                     `json:"pl01_extra_annual"`
	F2Accepted         *bool                     `json:"f2_accepted"`
	MBReady            bool                      `json:"mb_ready"`
	SplitAllowed       *bool                     `json:"split_allowed"`
	InitialQtyRequired int64                     `json:"initial_qty_required"`
	MasilReleased      int64                     `json:"masil_released"`
	NewOrders          map[string]int64          `json:"new_orders"`
	Options            map[string]OptionDecision `json:"options"`
	// Opt01NoValidation은 비교 시나리오 표시다(이슈 #29). 원본 OPT-01 노트 "원료 재계약·처방 재검증 불필요"를 적용해
	// OPT-01 적격 기간을 0주로 둔다. 기본(제출 기준)은 false이며, 엔진이 비교용으로만 켜서 한 번 더 계산한다.
	Opt01NoValidation bool `json:"opt01_no_validation,omitempty"`
	// R13: 이탈 의심 고객(AT_RISK)의 능력은 영업 임원 확인(CR-09) 전에는 C-201에 돌려주지 않는다.
	// 회신이 "계약·예측 수정 확인"이면 제품군별 연간 반환 수량만 별도 공급 출처(CUST-REL)로 넣는다.
	ChurnAnswered bool             `json:"churn_answered,omitempty"`
	ChurnRevised  bool             `json:"churn_revised,omitempty"`
	ChurnRelease  map[string]int64 `json:"churn_release,omitempty"`
}

// DefaultItems는 rfp_c201 표의 바이어 제시안이다(구조화가 끝나기 전 기본값이 아니라, 시드·검증용).
func DefaultItems() []RFPItem {
	return []RFPItem{{ProductBasic, 6_000_000, 11.80}, {ProductDerma, 1_500_000, 15.80}}
}

// DefaultScenario는 명세 §7 가정의 기본값이다(기준일 2026-07-01).
func DefaultScenario() Scenario { return SessionScenario(DefaultAsOf) }

// SessionScenario는 기준일 D에서 시작하는 시나리오다. 이사회 = D + 5일, 납품 개시 가정(A-04) = D 뒤 3개월째 달.
// D = 2026-07-01이면 이사회 2026-07-06, 개시 2026-10으로 기존 값과 같다.
func SessionScenario(asOf string) Scenario {
	d := mustDate(asOf)
	return Scenario{
		AsOf: asOf, BoardDate: fmtDate(d.AddDate(0, 0, BoardLagDays)), SimDate: asOf, Items: DefaultItems(),
		DeliveryStart: d.AddDate(0, 3, 0).Format("2006-01"), ContractMonths: 12, Validation: "A",
		PenaltyBasis: "backlog", CapacityPeriod: "annual", NewOrders: map[string]int64{},
		Options: map[string]OptionDecision{"OPT-01": {Status: OptProposed}, "OPT-02": {Status: OptProposed}},
	}
}

// LineEval은 라인별 여유 계산 결과다(R1).
type LineEval struct {
	Line
	SpareAnnual    int64 `json:"spare_annual"`
	MasilProtected int64 `json:"masil_protected"`
	MasilDeducted  int64 `json:"masil_deducted"`
	NewOrders      int64 `json:"new_orders"`
	BaseAvail      int64 `json:"base_avail_annual"`
	BaseMonthly    int64 `json:"base_monthly"`
	MasilDeficit   int64 `json:"masil_deficit"`
}

// OptionEval은 증설·전환안의 적격 능력일 계산 결과다(R3).
type OptionEval struct {
	CapexRow
	Status          string   `json:"status"`
	DecisionDate    string   `json:"decision_date"`
	DateAssumed     bool     `json:"decision_date_assumed"`
	InstallDate     string   `json:"install_date"`
	QualifiedDate   string   `json:"qualified_date"`
	StartMonth      string   `json:"start_month"`
	Monthly         int64    `json:"monthly_qty"`
	ValidationWeeks int      `json:"validation_weeks"`
	Pending         []string `json:"pending_requests"`
	Conditional     bool     `json:"conditional"`
	Usable          bool     `json:"usable"`
	Phase           string   `json:"phase"`
	PaybackMonths   float64  `json:"payback_months"`
}

// Supply는 한 달 공급의 출처별 수량이다.
type Supply struct {
	Basis       string `json:"basis"` // base | OPT-02 | OPT-01 | MASIL-ADJ
	Qty         int64  `json:"qty"`
	Conditional bool   `json:"conditional"`
}

// MonthRow는 제품×월 공급·부족·밀린 물량이다(R7·R8).
type MonthRow struct {
	Month    string   `json:"month"`
	Demand   int64    `json:"demand"`
	Supplies []Supply `json:"supplies"`
	Supply   int64    `json:"supply"`
	Firm     int64    `json:"firm"`
	Schedule int64    `json:"schedule"`
	Short    int64    `json:"short"`
	Backlog  int64    `json:"backlog"`
	Penalty  float64  `json:"penalty_krw"`
}

// ProductEval은 제품별 평가다.
type ProductEval struct {
	ProductID       string     `json:"product_id"`
	LineID          string     `json:"line_id"`
	AnnualQty       int64      `json:"annual_qty"`
	UnitPriceUSD    float64    `json:"unit_price_usd"`
	UnitKRW         float64    `json:"unit_krw"`
	MaterialPerEA   float64    `json:"material_per_ea"`
	CogsPerEA       float64    `json:"cogs_per_ea"`
	ContribPerEA    float64    `json:"contrib_per_ea"`
	MonthlyDemand   int64      `json:"monthly_demand"`
	Months          []MonthRow `json:"months"`
	ScheduleTotal   int64      `json:"schedule_total"`
	FirmTotal       int64      `json:"firm_total"`
	ShortTotal      int64      `json:"short_total"`
	CoveragePct     float64    `json:"coverage_pct"`
	PenaltyBacklog  float64    `json:"penalty_backlog_krw"` // 전량 약속 시 지연분 기준 노출
	PerLotMonth     float64    `json:"per_lot_month_krw"`   // 첫 달 부족분 1개월 지연당
	RevenueRequest  float64    `json:"revenue_request_krw"` // 요구 전량 기준 연매출
	ContribRequest  float64    `json:"contrib_request_krw"`
	RevenueSchedule float64    `json:"revenue_schedule_krw"` // 램프 제안 기준
	ContribSchedule float64    `json:"contrib_schedule_krw"`
	BreakEvenFX     float64    `json:"break_even_fx"`
}

// Commitments는 기존 약속 요약이다(R1 보호 목록, A-14 검산).
type Commitments struct {
	InProgressCount int                         `json:"in_progress_count"`
	InProgressQty   int64                       `json:"in_progress_qty"`
	ByProduct       map[string]int64            `json:"by_product"`
	CountByProduct  map[string]int              `json:"count_by_product"`
	EstDue          map[string]map[string]int64 `json:"est_due"` // 제품 → 월 → 수량(조건부 리드타임)
	T12MonthlyProd  map[string]int64            `json:"t12_monthly_prod"`
	MasilSPOs       []SPO                       `json:"masil_spos"`
}

// Flag는 판단 카드에 올릴 미확인·위험 항목이다.
type Flag struct {
	Code    string `json:"code"`
	Level   string `json:"level"` // 미확인 | 위험 | 차단
	Message string `json:"message"`
	Rule    string `json:"rule"`
}

// Evaluation은 한 시점의 엔진 계산 결과다.
type Evaluation struct {
	Scenario        Scenario               `json:"scenario"`
	Cutoff          string                 `json:"cutoff"`          // 이 평가가 쓴 기록의 마지막 날
	FutureSnapshot  bool                   `json:"future_snapshot"` // 능력 스냅샷이 기준일보다 뒤 자료(T2)
	FX              float64                `json:"fx"`
	FXDate          string                 `json:"fx_date"`
	Lines           []LineEval             `json:"lines"`
	F1WeightedUtil  float64                `json:"f1_weighted_util"`
	F1SimpleUtil    float64                `json:"f1_simple_util"`
	Options         []OptionEval           `json:"options"`
	Products        map[string]ProductEval `json:"products"`
	RevenueAt       map[string]float64     `json:"revenue_at"` // "0","-5","-10" 요구 전량
	ContribAt       map[string]float64     `json:"contrib_at"`
	ContractAnnual  float64                `json:"contract_annual_krw"`
	PenaltyContract float64                `json:"penalty_contract_krw"` // 계약 총액 기준(지연 달 수 × 3% × 연 계약액)
	PenaltyMonthly  float64                `json:"penalty_contract_monthly_krw"`
	FXDown100Rev    float64                `json:"fx_down100_revenue_krw"`
	FXDown100Contr  float64                `json:"fx_down100_contrib_krw"`
	InitialGap      int64                  `json:"initial_gap"` // 바이어 첫 달 요구 − 공급
	Commitments     Commitments            `json:"commitments"`
	Flags           []Flag                 `json:"flags"`
	CompareOPT01    *OptCompare            `json:"compare_opt01,omitempty"` // 비교 시나리오(원본 '재검증 불필요'), 표시용
	Churn           []ChurnSignal          `json:"churn"`                   // R12 이탈 신호(매출 비중 10% 이상 고객)
}

// Evaluate는 원본과 시나리오로 평가를 만든다. 같은 입력이면 항상 같은 결과다.
// 기본 시나리오 평가에는 OPT-01 비교 시나리오(compare.go) 결과를 붙인다. 판단·제출은 기본 시나리오로만 한다.
func Evaluate(b *Base, sc Scenario) *Evaluation {
	ev := evaluate(b, sc)
	if !sc.Opt01NoValidation {
		alt := sc
		alt.Opt01NoValidation = true
		ev.CompareOPT01 = compareOPT01(ev, evaluate(b, alt))
	}
	return ev
}

func evaluate(b *Base, sc Scenario) *Evaluation {
	if sc.ContractMonths <= 0 {
		sc.ContractMonths = 12
	}
	if len(sc.Items) == 0 {
		sc.Items = DefaultItems()
	}
	at := b.At(sc.SimDate) // 시계 날짜까지의 기록만 쓴다(데이터 끝에서 자름)
	if sc.BoardDate == "" {
		sc.BoardDate = SessionScenario(DefaultAsOf).BoardDate
	}
	ev := &Evaluation{Scenario: sc, FX: at.FX, FXDate: at.FXDate, Cutoff: at.Cutoff, FutureSnapshot: at.FutureSnapshot, Products: map[string]ProductEval{},
		RevenueAt: map[string]float64{}, ContribAt: map[string]float64{}}

	// R1: 라인 여유와 Masil 보호
	masil := map[string]int64{}
	for _, s := range at.SPOs {
		masil[s.ProductID] += s.Qty
	}
	var capF1, wF1, sF1 float64
	var nF1 int
	for _, l := range b.Lines {
		le := LineEval{Line: l}
		le.SpareAnnual = int64(math.Floor(float64(l.CapacityQty)*(100-l.UtilPct)/100 + 1e-9))
		if sc.CapacityPeriod == "monthly" {
			le.SpareAnnual *= 12
		}
		if l.ProductID == ProductBasic || l.ProductID == ProductDerma {
			le.MasilProtected = masil[l.ProductID]
			if !sc.MasilInUtil {
				le.MasilDeducted = le.MasilProtected
			}
			le.NewOrders = sc.NewOrders[l.ProductID]
			rest := le.SpareAnnual - le.MasilDeducted - le.NewOrders
			if rest < 0 {
				le.MasilDeficit = -rest
				rest = 0
			}
			le.BaseAvail = rest
			le.BaseMonthly = rest / 12
		}
		if l.PlantID == "F1" {
			capF1 += float64(l.CapacityQty)
			wF1 += float64(l.CapacityQty) * l.UtilPct
			sF1 += l.UtilPct
			nF1++
		}
		ev.Lines = append(ev.Lines, le)
	}
	if capF1 > 0 {
		ev.F1WeightedUtil = wF1 / capF1
		ev.F1SimpleUtil = sF1 / float64(nF1)
	}
	baseMonthly := map[string]int64{}
	for _, le := range ev.Lines {
		if le.ProductID == ProductBasic || le.ProductID == ProductDerma {
			baseMonthly[le.ProductID] = le.BaseMonthly
		}
	}
	if sc.PL01Confirmed && sc.PL01ExtraAnnual > 0 {
		baseMonthly[ProductBasic] += sc.PL01ExtraAnnual / 12
	}

	// R9 단가·원가
	items := map[string]RFPItem{}
	var usdTotal float64
	for _, it := range sc.Items {
		items[it.ProductID] = it
		usdTotal += float64(it.AnnualQty) * it.UnitPriceUSD
	}
	unit := func(p string, cut float64) float64 { return items[p].UnitPriceUSD * (1 + cut) * at.FX }
	contrib := func(p string, cut float64) float64 { return unit(p, cut) - at.MatCost[p].PerEA }
	for _, c := range []struct {
		k   string
		cut float64
	}{{"0", 0}, {"-5", -0.05}, {"-10", -0.10}} {
		var rev, con float64
		for _, it := range sc.Items {
			rev += float64(it.AnnualQty) * unit(it.ProductID, c.cut)
			con += float64(it.AnnualQty) * contrib(it.ProductID, c.cut)
		}
		ev.RevenueAt[c.k], ev.ContribAt[c.k] = rev, con
	}
	ev.ContractAnnual = usdTotal * (1 + sc.PriceCut) * at.FX
	ev.PenaltyMonthly = ev.ContractAnnual * PenaltyRate
	var matUSD float64
	for _, it := range sc.Items {
		if mc := at.MatCost[it.ProductID]; mc.USD && mc.EffectiveFX > 0 {
			matUSD += float64(it.AnnualQty) * mc.PerEA / mc.EffectiveFX
		}
	}
	ev.FXDown100Rev = usdTotal * (1 + sc.PriceCut) * 100
	ev.FXDown100Contr = ev.FXDown100Rev - matUSD*100

	// R3 적격 능력일
	vw := sc.ValidationWeeks
	if vw <= 0 {
		vw = 4
		if sc.Validation == "B" {
			vw = 12
		}
	}
	for _, cx := range b.Capex {
		ovw := vw
		if cx.OptionID == "OPT-01" && sc.Opt01NoValidation {
			ovw = 0 // 비교 시나리오: 원본 노트 "처방 재검증 불필요"
		}
		oe := OptionEval{CapexRow: cx, ValidationWeeks: ovw, Monthly: cx.AddedAnnual / 12}
		d := sc.Options[cx.OptionID]
		oe.Status = d.Status
		if oe.Status == "" {
			oe.Status = OptProposed
		}
		oe.DecisionDate, oe.DateAssumed = d.DecisionDate, false
		if oe.Status != OptApproved || oe.DecisionDate == "" {
			oe.DecisionDate, oe.DateAssumed = sc.BoardDate, true // A-09
		}
		inst := mustDate(oe.DecisionDate).AddDate(0, 0, 7*cx.LeadWeeks)
		qual := inst.AddDate(0, 0, 7*ovw)
		oe.InstallDate, oe.QualifiedDate = fmtDate(inst), fmtDate(qual)
		oe.StartMonth = time.Date(qual.Year(), qual.Month()+1, 1, 0, 0, 0, 0, time.UTC).Format("2006-01")
		if cx.OptionID == "OPT-02" {
			if sc.F2Accepted == nil || !*sc.F2Accepted {
				oe.Pending = append(oe.Pending, "CR-06")
			}
			if !sc.MBReady {
				oe.Pending = append(oe.Pending, "CR-07")
			}
		}
		oe.Usable = oe.Status != OptRejected && !(cx.OptionID == "OPT-02" && sc.F2Accepted != nil && !*sc.F2Accepted)
		oe.Conditional = oe.Status != OptApproved || len(oe.Pending) > 0
		oe.Phase = optionPhase(oe, sc.SimDate)
		if pc := contrib(ProductDerma, sc.PriceCut); pc > 0 {
			vol := min64(cx.AddedAnnual, items[ProductDerma].AnnualQty)
			oe.PaybackMonths = float64(cx.CapexKRW) / (float64(vol) * pc) * 12
		}
		ev.Options = append(ev.Options, oe)
	}

	// R7·R8 제품×월
	start, err := time.Parse("2006-01", sc.DeliveryStart)
	if err != nil {
		start = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	}
	delayMonths := map[string]bool{}
	for _, it := range sc.Items {
		p := it.ProductID
		pe := ProductEval{ProductID: p, AnnualQty: it.AnnualQty, UnitPriceUSD: it.UnitPriceUSD, UnitKRW: unit(p, sc.PriceCut),
			MaterialPerEA: at.MatCost[p].PerEA, CogsPerEA: at.CogsPerEA[p], ContribPerEA: contrib(p, sc.PriceCut), MonthlyDemand: it.AnnualQty / 12}
		if l, ok := b.LineOf(p); ok {
			pe.LineID = l.LineID
		}
		if usd := it.UnitPriceUSD * (1 + sc.PriceCut); usd > 0 {
			pe.BreakEvenFX = pe.MaterialPerEA / usd
		}
		var backlog int64
		for i := 0; i < sc.ContractMonths; i++ {
			m := start.AddDate(0, i, 0).Format("2006-01")
			row := MonthRow{Month: m, Demand: pe.MonthlyDemand}
			if q := baseMonthly[p]; q > 0 {
				row.Supplies = append(row.Supplies, Supply{Basis: "base", Qty: q})
			}
			if q := sc.ChurnRelease[p] / 12; sc.ChurnRevised && q > 0 { // R13: 영업 임원이 확인한 수량만
				row.Supplies = append(row.Supplies, Supply{Basis: "CUST-REL", Qty: q})
			}
			if p == ProductDerma {
				for _, oe := range ev.Options {
					if oe.Usable && m >= oe.StartMonth {
						row.Supplies = append(row.Supplies, Supply{Basis: oe.OptionID, Qty: oe.Monthly, Conditional: oe.Conditional})
					}
				}
				if i == 0 && sc.MasilReleased > 0 {
					row.Supplies = append(row.Supplies, Supply{Basis: "MASIL-ADJ", Qty: sc.MasilReleased})
				}
			}
			for _, s := range row.Supplies {
				row.Supply += s.Qty
				if !s.Conditional {
					row.Firm += s.Qty
				}
			}
			row.Schedule = min64(row.Demand, row.Supply)
			row.Short = row.Demand - row.Schedule
			backlog = max64(0, backlog+row.Demand-row.Supply)
			row.Backlog = backlog
			row.Penalty = PenaltyRate * float64(backlog) * pe.UnitKRW
			if backlog > 0 {
				delayMonths[m] = true
			}
			pe.ScheduleTotal += row.Schedule
			pe.FirmTotal += min64(row.Demand, row.Firm)
			pe.ShortTotal += row.Short
			pe.PenaltyBacklog += row.Penalty
			pe.Months = append(pe.Months, row)
		}
		if len(pe.Months) > 0 {
			pe.PerLotMonth = PenaltyRate * float64(pe.Months[0].Short) * pe.UnitKRW
		}
		if req := pe.MonthlyDemand * int64(sc.ContractMonths); req > 0 {
			pe.CoveragePct = float64(pe.ScheduleTotal) / float64(req) * 100
		}
		pe.RevenueRequest = float64(it.AnnualQty) * pe.UnitKRW
		pe.ContribRequest = float64(it.AnnualQty) * pe.ContribPerEA
		pe.RevenueSchedule = float64(pe.ScheduleTotal) * pe.UnitKRW
		pe.ContribSchedule = float64(pe.ScheduleTotal) * pe.ContribPerEA
		ev.Products[p] = pe
	}
	ev.PenaltyContract = float64(len(delayMonths)) * ev.PenaltyMonthly
	if d := ev.Products[ProductDerma]; len(d.Months) > 0 && sc.InitialQtyRequired > d.Months[0].Supply {
		ev.InitialGap = sc.InitialQtyRequired - d.Months[0].Supply
	}

	ev.Commitments = at.Commit
	ev.Churn = at.Churn
	ev.Flags = flags(ev, sc)
	return ev
}

func optionPhase(oe OptionEval, simDate string) string {
	switch {
	case !oe.Usable:
		if oe.Status == OptRejected {
			return "거절"
		}
		return "사용 불가(생산지 미수용)"
	case oe.Status != OptApproved:
		return oe.Status + "(승인 전)"
	case simDate < oe.InstallDate:
		return "설치중"
	case simDate < oe.QualifiedDate:
		return "재검증중"
	case len(oe.Pending) > 0:
		return "선행조건 대기"
	default:
		return "적격"
	}
}

// commitments는 잘라 낸 날 기준 진행 중 B2B 약속(가동률 포함, A-06)과 Masil 발주(보호, A-07)를 요약한다.
func (b *Base) commitments(a *AsOfData) Commitments {
	c := Commitments{ByProduct: map[string]int64{}, CountByProduct: map[string]int{}, EstDue: map[string]map[string]int64{},
		T12MonthlyProd: map[string]int64{}, MasilSPOs: a.SPOs}
	for _, o := range b.OpenOrders(a.Cutoff) {
		c.InProgressCount++
		c.InProgressQty += o.Qty
		c.ByProduct[o.ProductID] += o.Qty
		c.CountByProduct[o.ProductID]++
		due := EstDue(a, o) // A-14
		if due == "" {
			continue
		}
		if c.EstDue[o.ProductID] == nil {
			c.EstDue[o.ProductID] = map[string]int64{}
		}
		c.EstDue[o.ProductID][due[:7]] += o.Qty
	}
	for p, q := range a.T12Prod {
		c.T12MonthlyProd[p] = int64(math.RoundToEven(float64(q) / 12)) // 재현 스크립트(F03)와 같은 반올림
	}
	return c
}

// flags는 판단 카드의 미확인·위험·차단 항목이다. 무엇을 물어야 하는지는 여기서 정한다.
func flags(ev *Evaluation, sc Scenario) []Flag {
	var f []Flag
	add := func(code, level, rule, msg string, a ...any) {
		f = append(f, Flag{Code: code, Level: level, Rule: rule, Message: fmt.Sprintf(msg, a...)})
	}
	if ev.FutureSnapshot {
		add("FUTURE_SNAPSHOT", "미확인", "T2", "능력 스냅샷은 %s 자료다. 기준일(자료 %s까지)보다 뒤에 만들어진 자료를 쓰고 있다", DataEnd, ev.Cutoff)
	}
	if !sc.UnitConfirmed {
		add("UNIT_UNCONFIRMED", "미확인", "R4", "capacity_qty 단위가 원본에 없어 연간 EA로 읽었다(D6). 생산관리 확인 전까지 '단위 미확인'")
	}
	for _, le := range ev.Lines {
		if le.MasilDeficit > 0 {
			add("MASIL_OVER_SPARE", "미확인", "R1", "%s 기본 여유(연 %s)가 Masil 보호분(%s)보다 %s EA 적다. Masil 발주가 가동률에 들어 있는지 확인 필요", le.LineID, Commas(le.SpareAnnual), Commas(le.MasilProtected), Commas(le.MasilDeficit))
		}
	}
	if !sc.PL01Confirmed {
		b := ev.Products[ProductBasic]
		add("PL01_UNCONFIRMED", "차단", "R6", "PL-01 600만 EA를 소화할 수단이 원본에 없다. 확인 전 제안은 '미확정' 또는 월 %s EA 이하", Commas(firstSupply(b)))
	}
	for _, oe := range ev.Options {
		if oe.Usable && len(oe.Pending) > 0 {
			add("PREREQ_"+oe.OptionID, "미확인", "R3", "%s 능력은 선행조건(%s) 회신 전까지 조건부", oe.OptionID, strings.Join(oe.Pending, ", "))
		}
		if !oe.Usable && oe.Status != OptRejected {
			add("F2_REJECTED", "위험", "R3", "바이어가 F2 생산지를 받지 않아 %s 능력을 C-201에 쓸 수 없다", oe.OptionID)
		}
	}
	if !sc.StartConfirmed {
		add("START_ASSUMED", "미확인", "R7", "납품 개시월 %s는 가정(A-04). 바이어 확인 필요", sc.DeliveryStart)
	}
	if !sc.PenaltyConfirmed {
		add("PENALTY_BASIS", "미확인", "R8", "페널티 기준액·상한이 RFP에 없다. 지연분 기준과 계약 총액 기준(월 %.2f억)을 함께 본다", ev.PenaltyMonthly/1e8)
	}
	if ev.InitialGap > 0 {
		add("MASIL_CONFLICT", "차단", "R2", "바이어 첫 달 요구 %s EA가 공급보다 %s EA 많다. 남은 L-02 슬롯은 Masil 보호분뿐이라 침범할 수 없다", Commas(sc.InitialQtyRequired), Commas(ev.InitialGap))
	}
	if d := ev.Products[ProductDerma]; d.ShortTotal > 0 {
		add("PL02_SHORT", "위험", "R7", "PL-02 계약 기간 부족 %s EA(공급률 %.1f%%). 전량 약속 시 지연분 기준 노출 %.2f억", Commas(d.ShortTotal), d.CoveragePct, d.PenaltyBacklog/1e8)
	}
	for _, sg := range ev.Churn {
		switch sg.Status {
		case ChurnAtRisk:
			add("CHURN_"+sg.CustomerID, "위험", "R12", "이탈 경보 %s(%s부터). 출하는 아직 이어져 재무(출하 기준) 보고에는 정상으로 보인다. 원인은 단정하지 않는다(영업 회의록)", churnText(sg), sg.Since)
			if !sc.ChurnRevised {
				add("R13_NO_RETURN", "미확인", "R13", "%s 진행 주문 %d건(PL-01 %s · PL-02 %s EA)은 '검토'로만 표시하고 보호를 유지한다. 영업 임원 확인(CR-09) 전에는 C-201에 능력을 돌려주지 않는다(반환 0)",
					sg.CustomerID, sg.OpenOrders, Commas(sg.OpenQty[ProductBasic]), Commas(sg.OpenQty[ProductDerma]))
			}
		case ChurnWatch:
			add("CHURN_"+sg.CustomerID, "미확인", "R12", "이탈 관찰(WATCH) %s(%s부터). 단월 수주/출하가 2개월 연속 0.85 미만", churnText(sg), sg.Since)
		}
	}
	return f
}

// Commas는 수량을 천 단위 쉼표로 쓴다(화면 문장용).
func Commas(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		return "-" + s
	}
	return s
}

func firstSupply(pe ProductEval) int64 {
	if len(pe.Months) == 0 {
		return 0
	}
	return pe.Months[0].Supply
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
