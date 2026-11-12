package w05

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// 시뮬레이션 2(이슈 #39): 모의 가정 사건(E9)과 민감도 격자.
// 모의 가정은 시뮬레이션 세션에서만 받는다. 기준 세션(나중의 운영 세션)의 원본 제약을 기본 사실로 덮지 않기 위해서다
// (A05 §6 "원본 제약을 바꾸는 가정은 대안 시나리오로 분리"). 격자는 사건 없이 엔진 평가만 반복한다(어느 세션에서나 읽기 전용).

// EvSimAssumption은 모의 가정 사건이다(행위자 "시뮬레이터").
const EvSimAssumption = "E9_SIM_ASSUMPTION"

// SimAssumption은 모의 가정 묶음이다. 비어 있는 필드는 바꾸지 않는다.
type SimAssumption struct {
	DeliveryStart   *string           `json:"delivery_start,omitempty"`   // YYYY-MM
	ContractMonths  *int              `json:"contract_months,omitempty"`  // 1~36
	PriceCutPct     *float64          `json:"price_cut_pct,omitempty"`    // −50~0(%)
	FXShift         *float64          `json:"fx_shift,omitempty"`         // 기준일 종가에 더하는 원(−1000~1000), USD 원료비 1:1 연동
	CapacityPeriod  *string           `json:"capacity_period,omitempty"`  // annual | monthly
	MasilInUtil     *bool             `json:"masil_in_util,omitempty"`    // A-07을 끄면 true
	ValidationWeeks map[string]int    `json:"validation_weeks,omitempty"` // OPT-01·OPT-02 → 적격 주수(0~104)
	Options         map[string]string `json:"options,omitempty"`          // OPT-01·OPT-02 → 제안·승인·보류·거절
}

func validOpt(id string) bool { return id == "OPT-01" || id == "OPT-02" }

// apply는 모의 가정을 검증해 시나리오에 넣고, 바뀐 내용을 [모의 가정] 문장으로 돌려준다.
func (p SimAssumption) apply(sc *Scenario) ([]string, error) {
	var notes []string
	add := func(f string, a ...any) { notes = append(notes, "[모의 가정] "+fmt.Sprintf(f, a...)) }
	if p.DeliveryStart != nil {
		m, err := time.Parse("2006-01", *p.DeliveryStart)
		first := mustDate(sc.AsOf)
		if err != nil || m.Before(time.Date(first.Year(), first.Month(), 1, 0, 0, 0, 0, time.UTC)) || m.After(first.AddDate(3, 0, 0)) {
			return nil, fmt.Errorf("납품 개시월은 기준일 달부터 3년 안(YYYY-MM): %q", *p.DeliveryStart)
		}
		sc.DeliveryStart = *p.DeliveryStart
		add("납품 개시 %s", sc.DeliveryStart)
	}
	if p.ContractMonths != nil {
		if *p.ContractMonths < 1 || *p.ContractMonths > 36 {
			return nil, fmt.Errorf("계약 기간은 1~36개월: %d", *p.ContractMonths)
		}
		sc.ContractMonths = *p.ContractMonths
		add("계약 %d개월", sc.ContractMonths)
	}
	if p.PriceCutPct != nil {
		if *p.PriceCutPct > 0 || *p.PriceCutPct < -50 {
			return nil, fmt.Errorf("단가 인하는 −50~0%%: %v", *p.PriceCutPct)
		}
		sc.PriceCut = *p.PriceCutPct / 100
		add("단가 인하 %.1f%%", *p.PriceCutPct)
	}
	if p.FXShift != nil {
		if *p.FXShift < -1000 || *p.FXShift > 1000 {
			return nil, fmt.Errorf("환율 충격은 −1000~1000원: %v", *p.FXShift)
		}
		sc.FXShift = *p.FXShift
		add("환율 충격 %+.0f원(USD 원료비 1:1 연동)", sc.FXShift)
	}
	if p.CapacityPeriod != nil {
		if *p.CapacityPeriod != "annual" && *p.CapacityPeriod != "monthly" {
			return nil, fmt.Errorf("능력 단위는 annual·monthly: %q", *p.CapacityPeriod)
		}
		sc.CapacityPeriod = *p.CapacityPeriod
		add("capacity_qty 단위 %s", map[string]string{"annual": "연간", "monthly": "월간"}[sc.CapacityPeriod])
	}
	if p.MasilInUtil != nil {
		sc.MasilInUtil = *p.MasilInUtil
		add("Masil 발주 %s", map[bool]string{true: "가동률에 포함(여유에서 안 뺌)", false: "가동률 밖(여유에서 먼저 뺌)"}[sc.MasilInUtil])
	}
	for id, w := range p.ValidationWeeks {
		if !validOpt(id) || w < 0 || w > 104 {
			return nil, fmt.Errorf("적격 주수는 OPT-01·OPT-02, 0~104주: %s %d", id, w)
		}
		if sc.OptValidation == nil {
			sc.OptValidation = map[string]int{}
		}
		sc.OptValidation[id] = w
		add("%s 적격 %d주", id, w)
	}
	for id, st := range p.Options {
		if !validOpt(id) {
			return nil, fmt.Errorf("증설안은 OPT-01·OPT-02: %s", id)
		}
		switch st {
		case OptProposed, OptApproved, OptDeferred, OptRejected:
		default:
			return nil, fmt.Errorf("%s 결정 값은 제안·승인·보류·거절: %q", id, st)
		}
		if sc.Options == nil {
			sc.Options = map[string]OptionDecision{}
		}
		sc.Options[id] = OptionDecision{Status: st, DecisionDate: maxDate(sc.BoardDate, sc.SimDate)}
		add("%s %s(실제 이사회 결정 아님)", id, st)
	}
	return notes, nil
}

func decodeStrict(raw []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(v)
}

// cloneScenario는 맵까지 복사한다(격자 칸마다 독립 평가).
func cloneScenario(sc Scenario) Scenario {
	c := sc
	c.Items = append([]RFPItem(nil), sc.Items...)
	c.NewOrders = copyMap(sc.NewOrders)
	c.ChurnRelease = copyMap(sc.ChurnRelease)
	c.OptValidation = copyMap(sc.OptValidation)
	c.Options = copyMap(sc.Options)
	c.SimNotes = append([]string(nil), sc.SimNotes...)
	return c
}

func copyMap[K comparable, V any](m map[K]V) map[K]V {
	if m == nil {
		return nil
	}
	out := make(map[K]V, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// ---- 민감도 격자 ----

// SweepParams는 격자 변수와 설명이다.
var SweepParams = map[string]string{
	"price_cut_pct":          "단가 인하(%)",
	"delivery_start":         "납품 개시월",
	"fx_shift":               "환율 충격(원)",
	"contract_months":        "계약 기간(개월)",
	"capacity_period":        "능력 단위(annual·monthly)",
	"masil_in_util":          "Masil 가동률 포함(true·false)",
	"validation_weeks_opt01": "OPT-01 적격 주수",
	"validation_weeks_opt02": "OPT-02 적격 주수",
	"opt01_status":           "OPT-01 결정",
	"opt02_status":           "OPT-02 결정",
}

// SweepMetric은 격자 지표다.
type SweepMetric struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Unit  string `json:"unit"`
}

// SweepMetrics는 칸마다 내는 지표 목록이다.
var SweepMetrics = []SweepMetric{
	{"pl02_coverage_pct", "PL-02 공급률", "%"},
	{"pl02_short", "PL-02 부족(계약 기간)", "EA"},
	{"pl01_schedule", "PL-01 제안(계약 기간)", "EA"},
	{"penalty_backlog_krw", "전량 약속 시 지체상금(지연분)", "원"},
	{"penalty_contract_krw", "계약 총액 기준 지체상금", "원"},
	{"revenue_schedule_krw", "제안 기준 매출", "원"},
	{"contrib_schedule_krw", "제안 기준 공헌이익", "원"},
}

// SweepAxis는 격자의 한 축이다(값은 JSON 값 그대로).
type SweepAxis struct {
	Param  string            `json:"param"`
	Values []json.RawMessage `json:"values"`
}

// SweepResult는 격자 결과다. Cells[y][x]. Y가 없으면 한 줄이다.
type SweepResult struct {
	XParam  string                 `json:"x_param"`
	XLabel  string                 `json:"x_label"`
	XValues []string               `json:"x_values"`
	YParam  string                 `json:"y_param,omitempty"`
	YLabel  string                 `json:"y_label,omitempty"`
	YValues []string               `json:"y_values"`
	Metrics []SweepMetric          `json:"metrics"`
	Base    map[string]float64     `json:"base"` // 지금 세션 시나리오 그대로의 값
	Cells   [][]map[string]float64 `json:"cells"`
}

const maxSweepCells = 400

// paramAssumption은 격자 변수 하나를 모의 가정으로 바꾼다.
func paramAssumption(param string, raw json.RawMessage) (SimAssumption, string, error) {
	var p SimAssumption
	var s string
	var f float64
	var b bool
	num := func() error { return json.Unmarshal(raw, &f) }
	str := func() error { return json.Unmarshal(raw, &s) }
	var err error
	switch param {
	case "price_cut_pct":
		err = num()
		p.PriceCutPct = &f
	case "fx_shift":
		err = num()
		p.FXShift = &f
	case "contract_months":
		err = num()
		n := int(f)
		p.ContractMonths = &n
	case "validation_weeks_opt01", "validation_weeks_opt02":
		err = num()
		id := "OPT-01"
		if param == "validation_weeks_opt02" {
			id = "OPT-02"
		}
		p.ValidationWeeks = map[string]int{id: int(f)}
	case "delivery_start":
		err = str()
		p.DeliveryStart = &s
	case "capacity_period":
		err = str()
		p.CapacityPeriod = &s
	case "opt01_status", "opt02_status":
		err = str()
		id := "OPT-01"
		if param == "opt02_status" {
			id = "OPT-02"
		}
		p.Options = map[string]string{id: s}
	case "masil_in_util":
		err = json.Unmarshal(raw, &b)
		p.MasilInUtil = &b
	default:
		return p, "", fmt.Errorf("모르는 격자 변수: %q", param)
	}
	if err != nil {
		return p, "", fmt.Errorf("%s 값 형식 오류: %s", param, string(raw))
	}
	label := strings.Trim(string(raw), `"`)
	if param == "price_cut_pct" || param == "fx_shift" {
		label = strconv.FormatFloat(f, 'f', -1, 64)
	}
	return p, label, nil
}

func sweepMetrics(ev *Evaluation) map[string]float64 {
	p1, p2 := ev.Products[ProductBasic], ev.Products[ProductDerma]
	return map[string]float64{
		"pl02_coverage_pct":    p2.CoveragePct,
		"pl02_short":           float64(p2.ShortTotal),
		"pl01_schedule":        float64(p1.ScheduleTotal),
		"penalty_backlog_krw":  p1.PenaltyBacklog + p2.PenaltyBacklog,
		"penalty_contract_krw": ev.PenaltyContract,
		"revenue_schedule_krw": p1.RevenueSchedule + p2.RevenueSchedule,
		"contrib_schedule_krw": p1.ContribSchedule + p2.ContribSchedule,
	}
}

// Sweep은 시나리오 sc에서 변수 1~2개를 바꿔 가며 엔진 평가만 반복한다(사건을 남기지 않는다).
func Sweep(b *Base, sc Scenario, x SweepAxis, y *SweepAxis) (SweepResult, error) {
	if len(x.Values) == 0 {
		return SweepResult{}, fmt.Errorf("X축 값이 없다")
	}
	ys := []json.RawMessage{nil}
	if y != nil && y.Param != "" {
		if y.Param == x.Param {
			return SweepResult{}, fmt.Errorf("X·Y축 변수가 같다: %s", x.Param)
		}
		if len(y.Values) == 0 {
			return SweepResult{}, fmt.Errorf("Y축 값이 없다")
		}
		ys = y.Values
	} else {
		y = nil
	}
	if len(x.Values)*len(ys) > maxSweepCells {
		return SweepResult{}, fmt.Errorf("격자는 최대 %d칸: %d × %d", maxSweepCells, len(x.Values), len(ys))
	}
	res := SweepResult{XParam: x.Param, XLabel: SweepParams[x.Param], Metrics: SweepMetrics, Base: sweepMetrics(evaluate(b, sc))}
	if y != nil {
		res.YParam, res.YLabel = y.Param, SweepParams[y.Param]
	}
	for i, yv := range ys {
		row := make([]map[string]float64, 0, len(x.Values))
		ylabel := ""
		for _, xv := range x.Values {
			c := cloneScenario(sc)
			px, xlabel, err := paramAssumption(x.Param, xv)
			if err != nil {
				return SweepResult{}, err
			}
			if _, err := px.apply(&c); err != nil {
				return SweepResult{}, fmt.Errorf("X %s: %w", xlabel, err)
			}
			if y != nil {
				py, yl, err := paramAssumption(y.Param, yv)
				if err != nil {
					return SweepResult{}, err
				}
				if _, err := py.apply(&c); err != nil {
					return SweepResult{}, fmt.Errorf("Y %s: %w", yl, err)
				}
				ylabel = yl
			}
			if i == 0 {
				res.XValues = append(res.XValues, xlabel)
			}
			row = append(row, sweepMetrics(evaluate(b, c)))
		}
		res.YValues = append(res.YValues, ylabel)
		res.Cells = append(res.Cells, row)
	}
	return res, nil
}
