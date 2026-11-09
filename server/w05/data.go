// Package w05는 W05(대형 수주와 기존 고객 약속) 업무를 돌리는 결정 모델이다.
//
// 원본 CSV(data/raw, 수정 없는 복사본)를 바이너리에 넣고, 업무모델 명세의 규칙으로 계산하며,
// 사건(이벤트)을 순서대로 적용해 상태를 만든다. 명세: analysis/06_W05_수주약속/05_업무모델_명세.md(git 밖).
package w05

import (
	"bufio"
	"crypto/sha256"
	"embed"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed data/raw
var rawFS embed.FS

const rawRoot = "data/raw"

// 기준값(명세 §0·§7). 원본 문서에서 온 값만 상수로 둔다.
const (
	AsOf          = "2026-07-01" // 업무 기준일 [원본 A05]
	BoardDate     = "2026-07-06" // 이사회 = 기준일 + 5일 [원본 data_dictionary]
	SnapshotDate  = "2026-06-30" // 능력 스냅샷·데이터 마지막 날 [원본]
	T12From       = "2025-07"    // 최근 12개월
	T12To         = "2026-06"
	H1From        = "2026-01"
	PenaltyRate   = 0.03 // 납기 지연 페널티 월 3% [원본 rfp_c201]
	BidID         = "C-201"
	ProductBasic  = "PL-01"
	ProductDerma  = "PL-02"
	LineBasic     = "L-01"
	LineDerma     = "L-02"
	LineF2NewDerm = "F2-NEW-DERMA" // OPT-02 대상 [합성 매핑, 명세 A-10]
)

// VerifyManifest는 넣어 둔 원본 복사본이 SHA256SUMS와 같은지 확인한다(D4).
func VerifyManifest() (int, error) {
	f, err := rawFS.Open(rawRoot + "/SHA256SUMS")
	if err != nil {
		return 0, fmt.Errorf("SHA256SUMS 없음: %w", err)
	}
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		want, rel, ok := strings.Cut(line, "  ")
		if !ok {
			return n, fmt.Errorf("SHA256SUMS 형식 오류: %q", line)
		}
		b, err := rawFS.ReadFile(rawRoot + "/" + rel)
		if err != nil {
			return n, fmt.Errorf("%s 읽기 실패: %w", rel, err)
		}
		sum := sha256.Sum256(b)
		if got := hex.EncodeToString(sum[:]); got != want {
			return n, fmt.Errorf("%s 해시 불일치(원본과 다름)", rel)
		}
		n++
	}
	return n, sc.Err()
}

// RawText는 넣어 둔 문서 원문(rfp_c201.md 등)을 돌려준다.
func RawText(rel string) string {
	b, err := rawFS.ReadFile(rawRoot + "/" + rel)
	if err != nil {
		return ""
	}
	return string(b)
}

// readTable은 파이프(|) 구분 CSV를 헤더 이름 → 값 맵으로 읽는다(A05: UTF-8, 헤더 1행).
func readTable(rel string) ([]map[string]string, error) {
	f, err := rawFS.Open(rawRoot + "/" + rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.Comma = '|'
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("%s 헤더: %w", rel, err)
	}
	var rows []map[string]string
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", rel, err)
		}
		m := make(map[string]string, len(header))
		for i, h := range header {
			if i < len(rec) {
				m[h] = rec[i]
			}
		}
		rows = append(rows, m)
	}
	return rows, nil
}

// Line은 생산라인 능력 스냅샷이다(strat_capacity_cv + m_product_cv).
type Line struct {
	LineID      string  `json:"line_id"`
	PlantID     string  `json:"plant_id"`
	ProductID   string  `json:"product_id"`
	CapacityQty int64   `json:"capacity_qty"`
	UtilPct     float64 `json:"utilization_pct"`
	Snapshot    string  `json:"snapshot_date"`
	Dedicated   bool    `json:"dedicated"`
}

// CapexRow는 증설·전환안 원본 행이다(strat_capex_plan_cv).
type CapexRow struct {
	OptionID    string `json:"option_id"`
	Type        string `json:"type"`
	TargetText  string `json:"target_line_text"`
	LineID      string `json:"line_id"` // 우리가 붙인 매핑(A-10)
	CapexKRW    int64  `json:"capex_krw"`
	LeadWeeks   int    `json:"lead_weeks"`
	AddedAnnual int64  `json:"added_annual_qty"`
	Note        string `json:"note"`
}

// Order는 수주 1건이다(sales_orders_cv). 출하 여부는 Shipped로 붙인다.
type Order struct {
	OrderID    string `json:"order_id"`
	OrderDate  string `json:"order_date"`
	CustomerID string `json:"customer_id"`
	ProductID  string `json:"product_id"`
	Qty        int64  `json:"qty"`
	AmountKRW  int64  `json:"amount_krw"`
	Shipped    bool   `json:"-"`
}

// SPO는 Masil이 Corevia에 낸 발주다(sc_purchase_orders_ms + m_sku_ms.product_id).
type SPO struct {
	POID      string `json:"po_id"`
	SKU       string `json:"sku_id"`
	ProductID string `json:"product_id"`
	OrderDate string `json:"order_date"`
	Qty       int64  `json:"qty"`
	LeadWeeks int    `json:"lead_weeks"`
	ArriveBy  string `json:"arrive_by"` // 발주일 + 리드타임(A: 발주일 기산)
	PayDue    string `json:"payment_due_date"`
}

// MaterialCost는 C-201 원가 기준이다(rfp_c201 지정: applied_unit_cost_krw, F07).
type MaterialCost struct {
	MaterialID  string  `json:"material_id"`
	LastMonth   string  `json:"last_month"`
	AppliedKRW  float64 `json:"applied_krw_per_kg"`
	KgPerEA     float64 `json:"kg_per_ea_t12"`
	PerEA       float64 `json:"material_cost_per_ea"`
	EffectiveFX float64 `json:"effective_fx"` // applied ÷ 같은 달 USD 구매단가(USD 원료만)
	USD         bool    `json:"usd"`
}

// Base는 원본에서 읽은 W05 기초 자료다. 한 번 읽고 바꾸지 않는다.
type Base struct {
	Lines       []Line
	Capex       []CapexRow
	Orders      []Order
	LeadTimes   map[string][]int // 제품별 수주→출하 일수(정렬)
	SPOs        []SPO
	MatCost     map[string]MaterialCost
	CogsPerEA   map[string]float64 // 2026H1 IS 매출원가 ÷ 출하수량
	T12Prod     map[string]int64   // 최근 12개월 생산량
	FXDate      string
	FX          float64
	Files       int // 해시를 확인한 원본 파일 수
	CustomerIDs []string
	commit      *Commitments // 시나리오와 무관한 기존 약속 요약(한 번만 계산)
}

// LoadBase는 원본 복사본을 검사하고 읽는다.
func LoadBase() (*Base, error) {
	n, err := VerifyManifest()
	if err != nil {
		return nil, err
	}
	b := &Base{Files: n, LeadTimes: map[string][]int{}, MatCost: map[string]MaterialCost{}, CogsPerEA: map[string]float64{}, T12Prod: map[string]int64{}}

	prod, err := readTable("01_corevia/master/m_product_cv.csv")
	if err != nil {
		return nil, err
	}
	mainMat := map[string]string{}
	dedicated := map[string]bool{}
	for _, r := range prod {
		mainMat[r["product_id"]] = r["main_material_id"]
		dedicated[r["product_id"]] = r["is_f1_dedicated"] == "True"
	}

	capRows, err := readTable("01_corevia/production/strat_capacity_cv.csv")
	if err != nil {
		return nil, err
	}
	for _, r := range capRows {
		b.Lines = append(b.Lines, Line{
			LineID: r["line_id"], PlantID: r["plant_id"], ProductID: r["product_id"],
			CapacityQty: atoi(r["capacity_qty"]), UtilPct: atof(r["utilization_pct"]),
			Snapshot: r["snapshot_date"], Dedicated: dedicated[r["product_id"]],
		})
	}

	cx, err := readTable("01_corevia/production/strat_capex_plan_cv.csv")
	if err != nil {
		return nil, err
	}
	for _, r := range cx {
		line := LineDerma
		if r["option_type"] != "증설" {
			line = LineF2NewDerm
		}
		b.Capex = append(b.Capex, CapexRow{
			OptionID: r["option_id"], Type: r["option_type"], TargetText: r["target_line"], LineID: line,
			CapexKRW: atoi(r["capex_krw"]), LeadWeeks: int(atoi(r["lead_time_weeks"])),
			AddedAnnual: atoi(r["added_capacity_qty"]), Note: r["note"],
		})
	}
	sort.Slice(b.Capex, func(i, j int) bool { return b.Capex[i].OptionID < b.Capex[j].OptionID })

	orders, err := readTable("01_corevia/sales/sales_orders_cv.csv")
	if err != nil {
		return nil, err
	}
	ships, err := readTable("01_corevia/sales/sales_shipments_cv.csv")
	if err != nil {
		return nil, err
	}
	shipDate := map[string]string{}
	shipQtyH1 := map[string]int64{}
	for _, s := range ships {
		shipDate[s["order_id"]] = s["ship_date"]
		if ym := s["ship_date"][:7]; ym >= H1From && ym <= T12To {
			shipQtyH1[s["product_id"]] += atoi(s["shipped_qty"])
		}
	}
	cust := map[string]bool{}
	for _, r := range orders {
		o := Order{OrderID: r["order_id"], OrderDate: r["order_date"], CustomerID: r["customer_id"], ProductID: r["product_id"],
			Qty: atoi(r["order_qty"]), AmountKRW: atoi(r["order_amount_krw"])}
		if sd, ok := shipDate[o.OrderID]; ok {
			o.Shipped = true
			days := int(mustDate(sd).Sub(mustDate(o.OrderDate)).Hours() / 24)
			b.LeadTimes[o.ProductID] = append(b.LeadTimes[o.ProductID], days)
		}
		if o.CustomerID != "NONE" {
			cust[o.CustomerID] = true
		}
		b.Orders = append(b.Orders, o)
	}
	for p := range b.LeadTimes {
		sort.Ints(b.LeadTimes[p])
	}
	for c := range cust {
		b.CustomerIDs = append(b.CustomerIDs, c)
	}
	sort.Strings(b.CustomerIDs)

	skus, err := readTable("02_masil/master/m_sku_ms.csv")
	if err != nil {
		return nil, err
	}
	skuProd := map[string]string{}
	for _, r := range skus {
		skuProd[r["sku_id"]] = r["product_id"]
	}
	spos, err := readTable("02_masil/inventory/sc_purchase_orders_ms.csv")
	if err != nil {
		return nil, err
	}
	for _, r := range spos {
		lw := int(atoi(r["lead_time_weeks"]))
		b.SPOs = append(b.SPOs, SPO{POID: r["po_id"], SKU: r["sku_id"], ProductID: skuProd[r["sku_id"]], OrderDate: r["order_date"],
			Qty: atoi(r["order_qty"]), LeadWeeks: lw, ArriveBy: fmtDate(mustDate(r["order_date"]).AddDate(0, 0, 7*lw)), PayDue: r["payment_due_date"]})
	}

	if err := b.loadCost(mainMat, shipQtyH1); err != nil {
		return nil, err
	}

	fx, err := readTable("05_external/fx/ext_fx_daily.csv")
	if err != nil {
		return nil, err
	}
	for _, r := range fx {
		if r["date"] > b.FXDate && r["date"] <= SnapshotDate {
			b.FXDate, b.FX = r["date"], atof(r["usdkrw_close"])
		}
	}
	c := commitments(b)
	b.commit = &c
	return b, nil
}

// loadCost는 원료비/EA(F07), IS 매출원가/EA(F06), 최근 12개월 생산량(F03)을 계산한다.
func (b *Base) loadCost(mainMat map[string]string, shipQtyH1 map[string]int64) error {
	batches, err := readTable("01_corevia/production/prod_batch_log_cv.csv")
	if err != nil {
		return err
	}
	produced := map[string]int64{}
	for _, r := range batches {
		q := atoi(r["produced_qty"])
		produced[r["batch_id"]] = q
		if ym := r["production_date"][:7]; ym >= T12From && ym <= T12To {
			b.T12Prod[r["product_id"]] += q
		}
	}
	cons, err := readTable("01_corevia/production/production_material_consumption_cv.csv")
	if err != nil {
		return err
	}
	type acc struct {
		kg      float64
		ea      int64
		applied float64
	}
	byPM := map[string]map[string]*acc{} // product → ym → 합
	for _, r := range cons {
		p, ym := r["product_id"], r["consumption_date"][:7]
		if byPM[p] == nil {
			byPM[p] = map[string]*acc{}
		}
		a := byPM[p][ym]
		if a == nil {
			a = &acc{applied: math.Inf(1)}
			byPM[p][ym] = a
		}
		a.kg += atof(r["consumed_qty"])
		a.ea += produced[r["batch_id"]]
		a.applied = math.Min(a.applied, atof(r["applied_unit_cost_krw"]))
	}
	purchases, err := readTable("01_corevia/procurement/procurement_material_purchase_cv.csv")
	if err != nil {
		return err
	}
	for p, months := range byPM {
		last := ""
		var kg float64
		var ea int64
		for ym, a := range months {
			if ym > last {
				last = ym
			}
			if ym >= T12From && ym <= T12To {
				kg += a.kg
				ea += a.ea
			}
		}
		mc := MaterialCost{MaterialID: mainMat[p], LastMonth: last, AppliedKRW: months[last].applied}
		if ea > 0 {
			mc.KgPerEA = kg / float64(ea)
		}
		mc.PerEA = mc.AppliedKRW * mc.KgPerEA
		var lastUSD float64
		for _, r := range purchases {
			if r["material_id"] == mc.MaterialID && r["currency"] == "USD" && r["purchase_date"][:7] == last {
				lastUSD = math.Max(lastUSD, atof(r["unit_price_fx"]))
			}
		}
		if lastUSD > 0 {
			mc.USD = true
			mc.EffectiveFX = mc.AppliedKRW / lastUSD
		}
		b.MatCost[p] = mc
	}

	is, err := readTable("01_corevia/financials/fin_income_statement_cv.csv")
	if err != nil {
		return err
	}
	cogs := map[string]int64{}
	for _, r := range is {
		if ym := r["period_ym"]; ym >= H1From && ym <= T12To {
			cogs[r["product_id"]] += atoi(r["cogs_krw"])
		}
	}
	for p, q := range shipQtyH1 {
		if q > 0 {
			b.CogsPerEA[p] = float64(cogs[p]) / float64(q)
		}
	}
	return nil
}

// LineOf는 제품군을 만드는 기존 라인을 찾는다(라인 1개 = 제품군 1개).
func (b *Base) LineOf(product string) (Line, bool) {
	for _, l := range b.Lines {
		if l.ProductID == product {
			return l, true
		}
	}
	return Line{}, false
}

func atoi(s string) int64 {
	v, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return v
}

func atof(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}

func mustDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(fmt.Sprintf("날짜 형식 오류 %q", s))
	}
	return t
}

func parseDate(s string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02", s)
	return t, err == nil
}

func fmtDate(t time.Time) string { return t.Format("2006-01-02") }

// medianLow는 정렬된 값의 낮은 쪽 중앙값이다(Python statistics.median_low와 같다).
func medianLow(sorted []int) int {
	return sorted[(len(sorted)-1)/2]
}
