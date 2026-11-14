// Package w05는 W05(대형 수주와 기존 고객 약속) 업무를 돌리는 결정 모델이다.
//
// 원본 CSV(data/raw, 수정 없는 복사본)를 바이너리에 넣고, 업무모델 명세의 규칙으로 계산하며,
// 사건(이벤트)을 순서대로 적용해 상태를 만든다. 명세: analysis/06_W05_수주약속/05_업무모델_명세.md(git 밖).
//
// 기준일 재현(플랜 06_기준일_재현_플랜.md): 원본 표는 한 번 읽고, 날짜별 값(환율·원가·리드타임·진행 주문 등)은
// Base.At(날짜)가 그날까지의 기록만으로 계산한다. 그날 뒤의 기록은 판단에 쓰지 않는다(정답 비교에서만).
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
	"sync"
	"time"
)

//go:embed data/raw
var rawFS embed.FS

const rawRoot = "data/raw"

// 기준값(명세 §0·§7). 원본 문서에서 온 값만 상수로 둔다.
const (
	DefaultAsOf   = "2026-07-01" // 업무 기준일 [원본 A05]. 세션 기준일을 따로 주지 않으면 이 날짜
	DataEnd       = "2026-06-30" // 원본 데이터 마지막 날·능력 스냅샷 날 [원본]
	AsOfMin       = "2024-07-01" // 고를 수 있는 첫 기준일: 환율 시작일(T1)
	AsOfMax       = "2026-07-01" // 고를 수 있는 마지막 기준일(T1)
	BoardLagDays  = 5            // 이사회 = 기준일 + 5일 [원본 data_dictionary]
	PenaltyRate   = 0.03         // 납기 지연 페널티 월 3% [원본 rfp_c201]
	BidID         = "C-201"
	ProductBasic  = "PL-01"
	ProductDerma  = "PL-02"
	LineBasic     = "L-01"
	LineDerma     = "L-02"
	LineF2NewDerm = "F2-NEW-DERMA" // OPT-02 대상 [합성 매핑, 명세 A-10]
	openFrom      = "2024-01-01"   // 출하표 시작일. 그 전 수주는 출하 기록이 없어 진행 주문에서 뺀다(경계 효과)
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

// Order는 수주 1건이다(sales_orders_cv + 출하일).
type Order struct {
	OrderID    string `json:"order_id"`
	OrderDate  string `json:"order_date"`
	CustomerID string `json:"customer_id"`
	AgencyID   string `json:"agency_id"`
	Team       string `json:"sales_team"`
	ProductID  string `json:"product_id"`
	Qty        int64  `json:"qty"`
	AmountKRW  int64  `json:"amount_krw"`
	ShipDate   string `json:"-"` // 출하일(없으면 ""). 기준일 뒤 출하는 판단에 쓰지 않는다
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

// Batch는 생산 배치 1건이다(prod_batch_log_cv).
type Batch struct {
	BatchID   string `json:"batch_id"`
	ProductID string `json:"product_id"`
	Plant     string `json:"plant"`
	Date      string `json:"production_date"`
	Qty       int64  `json:"produced_qty"`
	QC        string `json:"qc_result"`
}

// MaterialCost는 C-201 원가 기준이다(rfp_c201 지정: applied_unit_cost_krw, F07).
type MaterialCost struct {
	MaterialID  string  `json:"material_id"`
	LastMonth   string  `json:"last_month"`
	AppliedKRW  float64 `json:"applied_krw_per_kg"`
	KgPerEA     float64 `json:"kg_per_ea_t12"`
	PerEA       float64 `json:"material_cost_per_ea"`
	EffectiveFX float64 `json:"effective_fx"` // applied ÷ USD 구매단가(USD 원료만)
	USD         bool    `json:"usd"`
}

type consRow struct {
	product, ym, date string
	kg, applied       float64
	ea                int64
}

type purchRow struct {
	material, ym, date, currency string
	price                        float64
}

type isRow struct {
	ym, product string
	cogs        int64
}

type fxRow struct {
	date string
	rate float64
}

// Customer는 B2B 고객 마스터다(m_customer_cv).
type Customer struct {
	ID          string  `json:"customer_id"`
	Name        string  `json:"customer_name"`
	SharePct    float64 `json:"revenue_share_pct"` // 전 기간 고정값(data_dictionary)
	CostLinked  bool    `json:"cost_linked_clause"`
	PaymentDays int     `json:"payment_terms_days"`
}

// Base는 원본에서 읽은 W05 표다. 한 번 읽고 바꾸지 않는다. 날짜별 값은 At이 만든다.
type Base struct {
	Lines       []Line
	Capex       []CapexRow
	Orders      []Order
	SPOs        []SPO
	Batches     []Batch
	Customers   []Customer
	Files       int // 해시를 확인한 원본 파일 수
	CustomerIDs []string
	booked      map[string]map[string]int64 // 고객 → 수주 달(order_date) → 수주 금액
	shipped     map[string]map[string]int64 // 고객 → 출하 달(ship_date) → 출하 매출(= 주문 금액, 17,987행 모두 같음)
	bookedQty   map[string]map[string]int64 // 고객|제품군 → 수주 달 → 수주 수량
	mainMat     map[string]string
	cons        []consRow
	purch       []purchRow
	is          []isRow
	fx          []fxRow // 날짜순

	mu    sync.Mutex
	cache map[string]*AsOfData
}

// AsOfData는 한 날짜(잘라 낸 날 Cutoff)까지의 기록만으로 계산한 값이다.
type AsOfData struct {
	Cutoff         string                  `json:"cutoff"` // min(날짜, DataEnd)
	FX             float64                 `json:"fx"`
	FXDate         string                  `json:"fx_date"`
	MatCost        map[string]MaterialCost `json:"material_cost"`
	CogsPerEA      map[string]float64      `json:"cogs_per_ea"`
	T12Prod        map[string]int64        `json:"t12_prod"`
	T12From        string                  `json:"t12_from"` // 최근 12개월(닫힌 달) 첫 달
	T12To          string                  `json:"t12_to"`   // 최근 12개월(닫힌 달) 마지막 달
	LeadTimes      map[string][]int        `json:"-"`
	SPOs           []SPO                   `json:"masil_spos"`
	Commit         Commitments             `json:"commitments"`
	Churn          []ChurnSignal           `json:"churn"`           // R12 이탈 신호(닫힌 달만)
	FutureSnapshot bool                    `json:"future_snapshot"` // 능력 스냅샷이 잘라 낸 날보다 뒤 자료(T2)
}

// LoadBase는 원본 복사본을 검사하고 읽는다.
func LoadBase() (*Base, error) {
	n, err := VerifyManifest()
	if err != nil {
		return nil, err
	}
	b := &Base{Files: n, mainMat: map[string]string{}, cache: map[string]*AsOfData{}}

	prod, err := readTable("01_corevia/master/m_product_cv.csv")
	if err != nil {
		return nil, err
	}
	dedicated := map[string]bool{}
	for _, r := range prod {
		b.mainMat[r["product_id"]] = r["main_material_id"]
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
	for _, s := range ships {
		shipDate[s["order_id"]] = s["ship_date"]
	}
	cust := map[string]bool{}
	for _, r := range orders {
		o := Order{OrderID: r["order_id"], OrderDate: r["order_date"], CustomerID: r["customer_id"], AgencyID: r["agency_id"],
			Team: r["sales_team"], ProductID: r["product_id"], Qty: atoi(r["order_qty"]), AmountKRW: atoi(r["order_amount_krw"]),
			ShipDate: shipDate[r["order_id"]]}
		if o.CustomerID != "NONE" {
			cust[o.CustomerID] = true
		}
		b.Orders = append(b.Orders, o)
	}
	sort.SliceStable(b.Orders, func(i, j int) bool { return b.Orders[i].OrderDate < b.Orders[j].OrderDate })
	for c := range cust {
		b.CustomerIDs = append(b.CustomerIDs, c)
	}
	sort.Strings(b.CustomerIDs)
	// R12 이탈 경보용 고객×월 수주·출하(금액). 판단에는 잘라 낸 날까지의 닫힌 달만 쓴다(churn.go)
	b.booked, b.shipped, b.bookedQty = map[string]map[string]int64{}, map[string]map[string]int64{}, map[string]map[string]int64{}
	addTo := func(m map[string]map[string]int64, k, ym string, v int64) {
		if m[k] == nil {
			m[k] = map[string]int64{}
		}
		m[k][ym] += v
	}
	for _, o := range b.Orders {
		if o.CustomerID == "NONE" {
			continue
		}
		addTo(b.booked, o.CustomerID, o.OrderDate[:7], o.AmountKRW)
		addTo(b.bookedQty, o.CustomerID+"|"+o.ProductID, o.OrderDate[:7], o.Qty)
		if o.ShipDate != "" {
			addTo(b.shipped, o.CustomerID, o.ShipDate[:7], o.AmountKRW)
		}
	}
	custRows, err := readTable("01_corevia/master/m_customer_cv.csv")
	if err != nil {
		return nil, err
	}
	for _, r := range custRows {
		b.Customers = append(b.Customers, Customer{ID: r["customer_id"], Name: r["customer_name"], SharePct: atof(r["revenue_share_pct"]),
			CostLinked: r["cost_linked_clause"] == "True", PaymentDays: int(atoi(r["payment_terms_days"]))})
	}
	sort.Slice(b.Customers, func(i, j int) bool { return b.Customers[i].ID < b.Customers[j].ID })

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

	batches, err := readTable("01_corevia/production/prod_batch_log_cv.csv")
	if err != nil {
		return nil, err
	}
	produced := map[string]int64{}
	for _, r := range batches {
		bt := Batch{BatchID: r["batch_id"], ProductID: r["product_id"], Plant: r["production_line"], Date: r["production_date"],
			Qty: atoi(r["produced_qty"]), QC: r["qc_result"]}
		produced[bt.BatchID] = bt.Qty
		b.Batches = append(b.Batches, bt)
	}
	sort.SliceStable(b.Batches, func(i, j int) bool { return b.Batches[i].Date < b.Batches[j].Date })

	cons, err := readTable("01_corevia/production/production_material_consumption_cv.csv")
	if err != nil {
		return nil, err
	}
	for _, r := range cons {
		b.cons = append(b.cons, consRow{product: r["product_id"], ym: r["consumption_date"][:7], date: r["consumption_date"],
			kg: atof(r["consumed_qty"]), applied: atof(r["applied_unit_cost_krw"]), ea: produced[r["batch_id"]]})
	}
	purchases, err := readTable("01_corevia/procurement/procurement_material_purchase_cv.csv")
	if err != nil {
		return nil, err
	}
	for _, r := range purchases {
		b.purch = append(b.purch, purchRow{material: r["material_id"], ym: r["purchase_date"][:7], date: r["purchase_date"],
			currency: r["currency"], price: atof(r["unit_price_fx"])})
	}
	is, err := readTable("01_corevia/financials/fin_income_statement_cv.csv")
	if err != nil {
		return nil, err
	}
	for _, r := range is {
		b.is = append(b.is, isRow{ym: r["period_ym"], product: r["product_id"], cogs: atoi(r["cogs_krw"])})
	}
	fx, err := readTable("05_external/fx/ext_fx_daily.csv")
	if err != nil {
		return nil, err
	}
	for _, r := range fx {
		b.fx = append(b.fx, fxRow{date: r["date"], rate: atof(r["usdkrw_close"])})
	}
	sort.Slice(b.fx, func(i, j int) bool { return b.fx[i].date < b.fx[j].date })
	return b, nil
}

// CutoffOf는 날짜를 데이터 끝에서 자른다. 기준일 2026-07-01이면 2026-06-30까지의 기록을 쓴다.
func CutoffOf(date string) string {
	if date == "" || date > DataEnd {
		return DataEnd
	}
	return date
}

// At은 날짜까지의 기록만으로 계산한 값을 돌려준다(같은 날짜는 캐시).
func (b *Base) At(date string) *AsOfData {
	c := CutoffOf(date)
	b.mu.Lock()
	defer b.mu.Unlock()
	if a, ok := b.cache[c]; ok {
		return a
	}
	if len(b.cache) >= 64 { // 날짜를 많이 오가도 메모리가 늘지 않게(한 항목 = 리드타임 표본 약 1.8만 개)
		b.cache = map[string]*AsOfData{}
	}
	a := b.compute(c)
	b.cache[c] = a
	return a
}

func (b *Base) compute(c string) *AsOfData {
	a := &AsOfData{Cutoff: c, MatCost: map[string]MaterialCost{}, CogsPerEA: map[string]float64{}, T12Prod: map[string]int64{},
		LeadTimes: map[string][]int{}, FutureSnapshot: c < DataEnd}

	for _, r := range b.fx { // 잘라 낸 날 이전 마지막 종가
		if r.date <= c {
			a.FXDate, a.FX = r.date, r.rate
		}
	}
	lastClosed := lastClosedMonth(c)

	// 최근 12개월(닫힌 달) 생산량
	t12 := monthsBack(lastClosed, 12)
	a.T12From, a.T12To = t12[len(t12)-1], t12[0]
	for _, bt := range b.Batches {
		if bt.Date <= c && inMonths(bt.Date[:7], t12) {
			a.T12Prod[bt.ProductID] += bt.Qty
		}
	}

	// 원료비/EA = 최근월 applied × 그 달까지 12개월 원단위(F07)
	type acc struct {
		kg      float64
		ea      int64
		applied float64
	}
	byPM := map[string]map[string]*acc{}
	for _, r := range b.cons {
		if r.date > c {
			continue
		}
		if byPM[r.product] == nil {
			byPM[r.product] = map[string]*acc{}
		}
		x := byPM[r.product][r.ym]
		if x == nil {
			x = &acc{applied: math.Inf(1)}
			byPM[r.product][r.ym] = x
		}
		x.kg += r.kg
		x.ea += r.ea
		x.applied = math.Min(x.applied, r.applied)
	}
	for p, months := range byPM {
		last := ""
		for ym := range months {
			if ym > last {
				last = ym
			}
		}
		win := monthsBack(last, 12)
		var kg float64
		var ea int64
		for ym, x := range months {
			if inMonths(ym, win) {
				kg += x.kg
				ea += x.ea
			}
		}
		mc := MaterialCost{MaterialID: b.mainMat[p], LastMonth: last, AppliedKRW: months[last].applied}
		if ea > 0 {
			mc.KgPerEA = kg / float64(ea)
		}
		mc.PerEA = mc.AppliedKRW * mc.KgPerEA
		// 실효환율: 최근월(없으면 그 전 가장 가까운 달)의 USD 구매단가 최댓값
		bestYM, price := "", 0.0
		for _, r := range b.purch {
			if r.material != mc.MaterialID || r.currency != "USD" || r.date > c || r.ym > last {
				continue
			}
			if r.ym > bestYM {
				bestYM, price = r.ym, r.price
			} else if r.ym == bestYM {
				price = math.Max(price, r.price)
			}
		}
		if price > 0 {
			mc.USD = true
			mc.EffectiveFX = mc.AppliedKRW / price
		}
		a.MatCost[p] = mc
	}

	// IS 매출원가/EA = 닫힌 최근 6개월 매출원가 ÷ 같은 달 출하수량(F06)
	h6 := monthsBack(lastClosed, 6)
	cogs := map[string]int64{}
	for _, r := range b.is {
		if inMonths(r.ym, h6) {
			cogs[r.product] += r.cogs
		}
	}
	shipQty := map[string]int64{}
	for _, o := range b.Orders {
		if o.ShipDate != "" && o.ShipDate <= c {
			days := int(mustDate(o.ShipDate).Sub(mustDate(o.OrderDate)).Hours() / 24)
			a.LeadTimes[o.ProductID] = append(a.LeadTimes[o.ProductID], days)
			if inMonths(o.ShipDate[:7], h6) {
				shipQty[o.ProductID] += o.Qty
			}
		}
	}
	for p := range a.LeadTimes {
		sort.Ints(a.LeadTimes[p])
	}
	for p, q := range shipQty {
		if q > 0 {
			a.CogsPerEA[p] = float64(cogs[p]) / float64(q)
		}
	}

	for _, s := range b.SPOs {
		if s.OrderDate <= c {
			a.SPOs = append(a.SPOs, s)
		}
	}
	a.Commit = b.commitments(a)
	a.Churn = b.churnSignals(a)
	return a
}

// OpenOrders는 잘라 낸 날 기준 진행 중 B2B 주문이다(2024년 이후 접수, 그날까지 미출하).
func (b *Base) OpenOrders(c string) []Order {
	var out []Order
	for _, o := range b.Orders {
		if o.OrderDate > c {
			break // 접수일 순 정렬
		}
		if o.CustomerID == "NONE" || o.OrderDate < openFrom || (o.ShipDate != "" && o.ShipDate <= c) {
			continue
		}
		out = append(out, o)
	}
	return out
}

// AgencyOpen은 진행 주문에서 뺀 대리점 주문(customer_id = NONE)의 건수·수량이다(가정 A-17, 표시용).
// OpenOrders와 같은 조건(2024년 이후 접수, 그날까지 미출하)에서 고객만 NONE인 주문이다.
func (b *Base) AgencyOpen(c string) (n int, qty int64) {
	for _, o := range b.Orders {
		if o.OrderDate > c {
			break
		}
		if o.CustomerID != "NONE" || o.OrderDate < openFrom || (o.ShipDate != "" && o.ShipDate <= c) {
			continue
		}
		n++
		qty += o.Qty
	}
	return n, qty
}

// EstDue는 A-14 추정 납기다: 주문일 + median_low{리드타임 | 리드타임 > 경과일}. 남은 분포가 없으면 "".
func EstDue(a *AsOfData, o Order) string {
	od := mustDate(o.OrderDate)
	age := int(mustDate(a.Cutoff).Sub(od).Hours() / 24)
	lt := a.LeadTimes[o.ProductID]
	i := sort.SearchInts(lt, age+1)
	if i >= len(lt) {
		return ""
	}
	return fmtDate(od.AddDate(0, 0, medianLow(lt[i:])))
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

// lastClosedMonth는 날짜까지 끝난 마지막 달이다(월말이면 그 달, 아니면 전달).
func lastClosedMonth(c string) string {
	t := mustDate(c)
	if t.AddDate(0, 0, 1).Month() != t.Month() {
		return c[:7]
	}
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0).Format("2006-01")
}

// monthsBack은 ym을 포함해 거꾸로 n개월이다.
func monthsBack(ym string, n int) []string {
	t, err := time.Parse("2006-01", ym)
	if err != nil {
		return nil
	}
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = t.AddDate(0, -i, 0).Format("2006-01")
	}
	return out
}

func inMonths(ym string, months []string) bool {
	for _, m := range months {
		if m == ym {
			return true
		}
	}
	return false
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
