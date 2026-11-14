package w05

// 화면 수치의 출처·단위·집계 기간과 원본 간 규모 차이(이슈 #27). 표시용이며 계산값을 바꾸지 않는다.
//
// 엔진(여유·C-201 가용·제안 일정)은 능력 스냅샷(strat_capacity_cv)을, 생산 라인 보드(월 생산·부하·상태)는
// 배치 실적(prod_batch_log_cv)과 진행 주문 기록을 쓴다. 두 원본은 연간·월간 어느 해석으로도 규모가 함께
// 맞지 않는다(analysis/06_W05_수주약속/07_기준_세계관_대조.md §4.4). 화면이 두 계열을 구분해 보이도록 이 값을 준다.

const (
	FileCapacity = "strat_capacity_cv"
	FileBatches  = "prod_batch_log_cv"
	FileOrders   = "sales_orders_cv · sales_shipments_cv"
)

// DataBasis는 화면 수치의 출처·단위·기간이다.
type DataBasis struct {
	SnapshotFile   string        `json:"snapshot_file"`
	SnapshotDate   string        `json:"snapshot_date"`
	CapacityUnit   string        `json:"capacity_unit"` // 연간 EA(D6 가정) | 월간 EA(회신)
	UnitConfirmed  bool          `json:"unit_confirmed"`
	FutureSnapshot bool          `json:"future_snapshot"`
	BatchFile      string        `json:"batch_file"`
	T12From        string        `json:"t12_from"`
	T12To          string        `json:"t12_to"`
	OrderFile      string        `json:"order_file"`
	Cutoff         string        `json:"cutoff"`
	AgencyOrders   int           `json:"agency_orders"` // 진행 주문에서 뺀 대리점 주문(A-17)
	AgencyQty      int64         `json:"agency_qty"`
	Gaps           []CapacityGap `json:"gaps"`
}

// CapacityGap은 라인별 배치 실적과 능력 스냅샷의 규모 차이다.
type CapacityGap struct {
	LineID       string  `json:"line_id"`
	PlantID      string  `json:"plant_id"`
	ProductID    string  `json:"product_id"`
	T12Monthly   int64   `json:"t12_monthly"`  // 배치 실적 월 평균(EA/월)
	CapacityQty  int64   `json:"capacity_qty"` // 능력 스냅샷 capacity_qty(단위 미기재)
	UtilPct      float64 `json:"utilization_pct"`
	AnnualMonth  float64 `json:"annual_monthly_cap"` // 연간으로 읽은 월 능력 = capacity ÷ 12
	RatioAnnual  float64 `json:"ratio_annual_pct"`   // 실적 ÷ (capacity ÷ 12) × 100
	RatioMonthly float64 `json:"ratio_monthly_pct"`  // 실적 ÷ capacity × 100
}

// Basis는 지금 시나리오(업무일)의 출처·기간과 라인별 규모 차이를 만든다.
func (s *State) Basis(b *Base) DataBasis {
	sc := s.Scenario
	at := b.At(sc.SimDate)
	unit := "연간 EA(원본에 단위 없음, D6 가정)"
	if sc.CapacityPeriod == "monthly" {
		unit = "월간 EA(생산관리 회신)"
	} else if sc.UnitConfirmed {
		unit = "연간 EA(생산관리 회신)"
	}
	d := DataBasis{SnapshotFile: FileCapacity, SnapshotDate: DataEnd, CapacityUnit: unit, UnitConfirmed: sc.UnitConfirmed,
		FutureSnapshot: at.FutureSnapshot, BatchFile: FileBatches, T12From: at.T12From, T12To: at.T12To, OrderFile: FileOrders,
		Cutoff: at.Cutoff}
	d.AgencyOrders, d.AgencyQty = b.AgencyOpen(at.Cutoff)
	for _, l := range b.Lines {
		g := CapacityGap{LineID: l.LineID, PlantID: l.PlantID, ProductID: l.ProductID, T12Monthly: at.Commit.T12MonthlyProd[l.ProductID],
			CapacityQty: l.CapacityQty, UtilPct: l.UtilPct, AnnualMonth: float64(l.CapacityQty) / 12}
		if g.AnnualMonth > 0 {
			g.RatioAnnual = float64(g.T12Monthly) / g.AnnualMonth * 100
		}
		if l.CapacityQty > 0 {
			g.RatioMonthly = float64(g.T12Monthly) / float64(l.CapacityQty) * 100
		}
		d.Gaps = append(d.Gaps, g)
	}
	return d
}
