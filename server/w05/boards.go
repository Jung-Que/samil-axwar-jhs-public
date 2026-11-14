package w05

import (
	"fmt"
	"sort"
	"time"
)

// 기준일 재현 보기(플랜 06_기준일_재현_플랜.md §4): 업무 레인 보드, 생산 라인 보드, 접수함, 정답 비교.
// 모두 시계 날짜까지의 기록(At)과 사건 루프 상태로 만든다. 판단 규칙은 바꾸지 않는다.

// LaneItem은 레인 칸의 한 줄이다. Source는 출처(원본 · 계산 · 실제 작동 · 모의)다.
type LaneItem struct {
	Text   string `json:"text"`
	Source string `json:"source"`
}

// LaneAction은 그 레인이 지금 넣을 수 있는(또는 막힌) 사건이다.
type LaneAction struct {
	Event     string `json:"event"`
	Label     string `json:"label"`
	Enabled   bool   `json:"enabled"`
	Reason    string `json:"reason,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// Lane은 업무 레인 하나의 네 칸이다.
type Lane struct {
	Role     string       `json:"role"`
	Incoming []LaneItem   `json:"incoming"`
	Can      []LaneAction `json:"can"`
	Doing    []LaneItem   `json:"doing"`
	Blocked  []LaneItem   `json:"blocked"`
}

// LaneRoles는 W05 업무 레인(BPMN 4개 + 확인 요청을 받는 구매·품질, D2의 Masil SCM)이다.
var LaneRoles = []string{"영업", "생산관리", "재무·전략", "CEO/이사회", "구매·품질", "Masil SCM"}

func laneOf(owner string) string {
	switch owner {
	case "그룹 재무":
		return "재무·전략"
	case "영업 임원":
		return "영업"
	}
	return owner
}

// Lanes는 업무 레인 보드다.
func (s *State) Lanes(b *Base) []Lane {
	at := b.At(s.Scenario.SimDate)
	c := at.Cutoff
	lanes := map[string]*Lane{}
	for _, r := range LaneRoles {
		lanes[r] = &Lane{Role: r}
	}
	add := func(role string, kind *[]LaneItem, src, f string, a ...any) {
		*kind = append(*kind, LaneItem{Text: fmt.Sprintf(f, a...), Source: src})
	}
	st := s.bidState()

	// 원본: 접수·출하·생산
	today, last7, q7 := 0, 0, int64(0)
	byLine7 := map[string]int{}
	for _, o := range b.Orders {
		if o.OrderDate > c {
			break
		}
		if d := daysBetween(o.OrderDate, c); d >= 0 && d < 7 {
			last7++
			q7 += o.Qty
			if l, ok := b.LineOf(o.ProductID); ok {
				byLine7[l.LineID]++
			}
			if d == 0 {
				today++
			}
		}
	}
	open := b.OpenOrders(c)
	due14, due7, q14 := 0, 0, int64(0)
	for _, o := range open {
		if due := EstDue(at, o); due != "" {
			if d := daysBetween(c, due); d <= 14 {
				due14++
				q14 += o.Qty
				if d <= 7 {
					due7++
				}
			}
		}
	}

	sales := lanes["영업"]
	add("영업", &sales.Incoming, "원본", "기록 마지막 날(%s) 접수 %d건 · 최근 7일 %d건 %s EA", c, today, last7, Commas(q7))
	if s.Bid != nil {
		add("영업", &sales.Incoming, "모의", "입찰 %s RFP 접수(%s) · 상태 %s · 마감 %s", s.Bid.ID, s.Bid.Received, st, s.Bid.Deadline)
	}
	add("영업", &sales.Doing, "원본·계산", "진행 중 주문 %d건 · 추정 납기 14일 안 %d건 %s EA", len(open), due14, Commas(q14))
	for _, sg := range at.Churn { // R12 이탈 신호(수주와 출하를 나눠 본다)
		if sg.Status == ChurnOK {
			continue
		}
		add("영업", &sales.Incoming, "계산", "이탈 %s %s", sg.Status, churnText(sg))
		if sg.Status == ChurnAtRisk && !s.Scenario.ChurnRevised {
			add("영업", &sales.Blocked, "계산", "R13: %s 능력은 영업 임원 확인(CR-09) 전 C-201 반환 금지(진행 주문 %d건 '검토', 보호 유지)", sg.CustomerID, sg.OpenOrders)
		}
	}

	prod := lanes["생산관리"]
	lineText := ""
	for _, l := range b.Lines {
		if n := byLine7[l.LineID]; n > 0 {
			lineText += fmt.Sprintf(" %s %d", l.LineID, n)
		}
	}
	add("생산관리", &prod.Incoming, "원본", "최근 7일 신규 주문 배정(라인별 건수):%s", lineText)
	for _, l := range b.Lines {
		done, typical := batchesThisMonth(b, l.ProductID, c), typicalBatches(b, l.ProductID, c)
		add("생산관리", &prod.Doing, "원본", "%s(%s) %s월 배치 %d/%d회 완료", l.LineID, l.ProductID, c[5:7], done, typical)
	}
	add("생산관리", &prod.Doing, "계산", "추정 납기 7일 안 출하 예정 %d건", due7)

	fin := lanes["재무·전략"]
	add("재무·전략", &fin.Incoming, "원본", "환율 %.2f(%s 종가) · 원료 단가 기준월 PL-01 %s · PL-02 %s", at.FX, at.FXDate,
		at.MatCost[ProductBasic].LastMonth, at.MatCost[ProductDerma].LastMonth)

	masil := lanes["Masil SCM"]
	if len(at.SPOs) == 0 {
		add("Masil SCM", &masil.Incoming, "원본", "기록 마지막 날(%s)까지 Corevia 앞 발주 없음(첫 발주 2026-06-22)", c)
	}
	for _, sp := range at.SPOs {
		if sp.ProductID == ProductBasic || sp.ProductID == ProductDerma {
			add("Masil SCM", &masil.Doing, "원본·합성 매핑", "%s %s %s EA → %s 입고 필요(보호 약속)", sp.POID, sp.ProductID, Commas(sp.Qty), sp.ArriveBy)
		}
	}

	ceo := lanes["CEO/이사회"]
	if st == BidSubmitted {
		add("CEO/이사회", &ceo.Incoming, "실제 작동", "상신된 입찰 %s 제안 v%d(이사회 %s)", s.Bid.ID, s.Bid.Version, s.Scenario.BoardDate)
	}

	// 사건 루프: 지금 넣을 수 있는 사건(상태 기계, 명세 §3)
	act := func(role, ev, label string, ok bool, reason string) {
		a := LaneAction{Event: ev, Label: label, Enabled: ok}
		if !ok {
			a.Reason = reason
		}
		lanes[role].Can = append(lanes[role].Can, a)
	}
	act("영업", EvRFP, "E1 RFP 접수", s.Bid == nil, "이미 접수된 입찰이 있습니다.")
	act("영업", EvForApprove, "E8 상신", st == BidReviewing || st == BidRereview, "검토중·재검토 상태에서만 할 수 있습니다(현재 "+st+").")
	act("영업", EvSubmit, "E7 제안 제출", st == BidApproved || st == BidConditional, "승인·조건부 승인 상태에서만 할 수 있습니다(현재 "+st+").")
	act("영업", EvBuyer, "E4 바이어 회신 입력", s.Bid != nil, "입찰이 접수된 뒤에 입력할 수 있습니다.")
	act("영업", EvNewOrder, "E5 신규 수주", true, "")
	act("CEO/이사회", EvDecision, "E2 승인 결정", st == BidSubmitted, "상신 상태에서만 입력할 수 있습니다(현재 "+st+").")
	for _, r := range s.Requests {
		role := laneOf(r.ToRole)
		if r.ToRole == "바이어" {
			if r.State != "반영" {
				add("영업", &sales.Blocked, "실제 작동", "%s 바이어 회신 대기(기한 %s): %s", r.ID, r.Due, r.Question)
			}
			continue
		}
		if _, ok := lanes[role]; !ok {
			continue
		}
		if r.State != "반영" {
			lanes[role].Can = append(lanes[role].Can, LaneAction{Event: EvReply, Label: r.ID + " 회신", Enabled: true, RequestID: r.ID})
			add(role, &lanes[role].Incoming, "실제 작동", "확인 요청 %s(기한 %s): %s", r.ID, r.Due, r.Question)
		}
		if r.State == "기한 초과" {
			add(role, &lanes[role].Blocked, "실제 작동", "%s 기한 초과(%s) — 판단 보류: %v", r.ID, r.Due, r.Blocks)
		}
	}
	for _, t := range s.Tasks {
		if t.State != "열림" {
			continue
		}
		if l, ok := lanes[laneOf(t.Owner)]; ok {
			add(l.Role, &l.Doing, "실제 작동", "%s %s(~%s)", t.ID, t.Title, t.Due)
		}
	}
	for _, d := range s.Decisions {
		if d.Freshness == "낡음" && d == s.Decisions[len(s.Decisions)-1] {
			add("영업", &sales.Blocked, "실제 작동", "결정 %s 낡음(승인 뒤 내용 바뀜) → 재상신 필요", d.ID)
		}
	}
	for _, m := range s.LastBlocks {
		add("영업", &sales.Blocked, "실제 작동", "제출 차단: %s", m)
	}

	out := make([]Lane, 0, len(LaneRoles))
	for _, r := range LaneRoles {
		out = append(out, *lanes[r])
	}
	return out
}

// MonthLoad는 라인의 한 달 약속 부하다.
type MonthLoad struct {
	Month  string `json:"month"`
	Orders int64  `json:"orders"` // 진행 주문(추정 납기 기준)
	C201   int64  `json:"c201"`   // C-201 가예약
}

// LineRow는 생산 라인 보드 한 줄이다.
type LineRow struct {
	LineID         string      `json:"line_id"`
	PlantID        string      `json:"plant_id"`
	ProductID      string      `json:"product_id"`
	Kind           string      `json:"kind"` // existing | new
	Dedicated      bool        `json:"dedicated"`
	CapacityAnnual int64       `json:"capacity_annual"`
	MonthlyCap     int64       `json:"monthly_cap"`
	UtilPct        float64     `json:"utilization_pct"`
	SpareAnnual    int64       `json:"spare_annual"`
	FutureSnapshot bool        `json:"future_snapshot"`
	BatchesDone    int         `json:"batches_done"`
	BatchesTypical int         `json:"batches_typical"`
	LastBatchDate  string      `json:"last_batch_date"`
	LastBatchQty   int64       `json:"last_batch_qty"`
	HoldBatches90  int         `json:"hold_batches_90d"`
	T12Monthly     int64       `json:"t12_monthly"`
	OpenOrders     int         `json:"open_orders"`
	OpenQty        int64       `json:"open_qty"`
	MasilProtected int64       `json:"masil_protected"`
	Load           []MonthLoad `json:"load"`
	Status         string      `json:"status"` // 여유 | 포화 | 초과 | 신규
	Note           string      `json:"note,omitempty"`
}

// LineBoard는 생산 라인 보드다(라인 1개 = 제품군 1개, OPT-02 승인 시 F2 신규 더마 라인 추가).
func (s *State) LineBoard(b *Base) []LineRow {
	at := b.At(s.Scenario.SimDate)
	c := at.Cutoff
	months := nextMonths(c, 3)
	ev := s.Eval
	spare := map[string]int64{}
	masil := map[string]int64{}
	for _, le := range ev.Lines {
		spare[le.LineID] = le.SpareAnnual
		masil[le.LineID] = le.MasilProtected
	}
	open := b.OpenOrders(c)
	var out []LineRow
	for _, l := range b.Lines {
		r := LineRow{LineID: l.LineID, PlantID: l.PlantID, ProductID: l.ProductID, Kind: "existing", Dedicated: l.Dedicated,
			CapacityAnnual: l.CapacityQty, MonthlyCap: l.CapacityQty / 12, UtilPct: l.UtilPct, SpareAnnual: spare[l.LineID],
			FutureSnapshot: at.FutureSnapshot, BatchesDone: batchesThisMonth(b, l.ProductID, c), BatchesTypical: typicalBatches(b, l.ProductID, c),
			T12Monthly: at.Commit.T12MonthlyProd[l.ProductID], MasilProtected: masil[l.LineID]}
		for _, bt := range b.Batches {
			if bt.ProductID != l.ProductID || bt.Date > c {
				continue
			}
			r.LastBatchDate, r.LastBatchQty = bt.Date, bt.Qty
			if bt.QC != "합격" && daysBetween(bt.Date, c) < 90 {
				r.HoldBatches90++
			}
		}
		load := map[string]*MonthLoad{}
		for _, m := range months {
			load[m] = &MonthLoad{Month: m}
		}
		for _, o := range open {
			if o.ProductID != l.ProductID {
				continue
			}
			r.OpenOrders++
			r.OpenQty += o.Qty
			if due := EstDue(at, o); due != "" {
				if x := load[due[:7]]; x != nil {
					x.Orders += o.Qty
				}
			}
		}
		for _, rs := range s.Reservations {
			if rs.ProductID == l.ProductID && reservationLine(rs.Basis) == l.LineID {
				if x := load[rs.Month]; x != nil {
					x.C201 += rs.Qty
				}
			}
		}
		for _, m := range months {
			r.Load = append(r.Load, *load[m])
		}
		r.Status = loadStatus(r.Load, r.T12Monthly)
		out = append(out, r)
	}
	for _, oe := range ev.Options {
		if oe.LineID != LineF2NewDerm || !oe.Usable || oe.Status != OptApproved {
			continue
		}
		r := LineRow{LineID: LineF2NewDerm, PlantID: "F2", ProductID: ProductDerma, Kind: "new", CapacityAnnual: oe.AddedAnnual,
			MonthlyCap: oe.Monthly, Status: "신규", Note: fmt.Sprintf("OPT-02 %s · 설치 %s · 적격 %s(산입 %s~)", oe.Phase, oe.InstallDate, oe.QualifiedDate, oe.StartMonth)}
		for _, m := range months {
			x := MonthLoad{Month: m}
			for _, rs := range s.Reservations {
				if rs.ProductID == ProductDerma && rs.Month == m && reservationLine(rs.Basis) == LineF2NewDerm {
					x.C201 += rs.Qty
				}
			}
			r.Load = append(r.Load, x)
		}
		out = append(out, r)
	}
	return out
}

func reservationLine(basis string) string {
	if basis == "OPT-02" {
		return LineF2NewDerm
	}
	return LineDerma // base·OPT-01·Masil 조정은 L-02(PL-02). PL-01 가예약은 base뿐이라 아래에서 제품으로 걸러진다
}

// loadStatus는 다음 달 약속 부하 ÷ 최근 12개월 월평균 생산으로 상태를 붙인다(표시용, 판단 규칙 아님).
func loadStatus(load []MonthLoad, t12 int64) string {
	if len(load) == 0 || t12 <= 0 {
		return "정보 없음"
	}
	r := float64(load[0].Orders+load[0].C201) / float64(t12)
	switch {
	case r > 1:
		return "초과"
	case r >= 0.85:
		return "포화"
	default:
		return "여유"
	}
}

func batchesThisMonth(b *Base, product, c string) int {
	n := 0
	for _, bt := range b.Batches {
		if bt.ProductID == product && bt.Date[:7] == c[:7] && bt.Date <= c {
			n++
		}
	}
	return n
}

// typicalBatches는 닫힌 달들의 월 배치 횟수 최빈값이다(관행, [계산]).
func typicalBatches(b *Base, product, c string) int {
	lc := lastClosedMonth(c)
	per := map[string]int{}
	for _, bt := range b.Batches {
		if bt.ProductID == product && bt.Date[:7] <= lc {
			per[bt.Date[:7]]++
		}
	}
	freq := map[int]int{}
	best, bestN := 0, 0
	for _, n := range per {
		freq[n]++
		if freq[n] > bestN || (freq[n] == bestN && n > best) {
			best, bestN = n, freq[n]
		}
	}
	return best
}

// InboxOrder는 접수함의 수주 한 건이다.
type InboxOrder struct {
	Order
	LineID   string `json:"line_id"`
	Status   string `json:"status"` // 진행 | 출하 완료 | 대리점(출하 기록 없음)
	ShipDate string `json:"ship_date,omitempty"`
	EstDue   string `json:"est_due,omitempty"`
}

// Inbox는 접수함이다.
type Inbox struct {
	AsOf         string           `json:"as_of"`
	SimDate      string           `json:"sim_date"`
	Cutoff       string           `json:"cutoff"`
	Days         int              `json:"days"`
	Today        int              `json:"today"`
	Last7        int              `json:"last7"`
	Last30       int              `json:"last30"`
	WindowCount  int              `json:"window_count"`
	QtyByProduct map[string]int64 `json:"qty_by_product"`
	CountByTeam  map[string]int   `json:"count_by_team"`
	Shipped7     int              `json:"shipped7"`
	OpenCount    int              `json:"open_count"`
	Orders       []InboxOrder     `json:"orders"`
	Truncated    bool             `json:"truncated"`
	Masil        []SPO            `json:"masil"`
	Bid          *Bid             `json:"bid"`
}

// Inbox는 잘라 낸 날 기준 최근 days일(그날 포함) 접수를 최신순으로 돌려준다.
func (s *State) Inbox(b *Base, days, limit int) Inbox {
	at := b.At(s.Scenario.SimDate)
	c := at.Cutoff
	in := Inbox{AsOf: s.Scenario.AsOf, SimDate: s.Scenario.SimDate, Cutoff: c, Days: days, QtyByProduct: map[string]int64{},
		CountByTeam: map[string]int{}, Masil: at.SPOs, Bid: s.Bid, OpenCount: len(b.OpenOrders(c))}
	for i := len(b.Orders) - 1; i >= 0; i-- {
		o := b.Orders[i]
		if o.OrderDate > c {
			continue
		}
		d := daysBetween(o.OrderDate, c)
		if d >= 30 && d >= days {
			break
		}
		switch {
		case d == 0:
			in.Today++
			fallthrough
		case d < 7:
			in.Last7++
			fallthrough
		case d < 30:
			in.Last30++
		}
		if d >= days {
			continue
		}
		in.WindowCount++
		in.QtyByProduct[o.ProductID] += o.Qty
		in.CountByTeam[o.Team]++
		if len(in.Orders) >= limit {
			in.Truncated = true
			continue
		}
		io := InboxOrder{Order: o}
		if l, ok := b.LineOf(o.ProductID); ok {
			io.LineID = l.LineID
		}
		switch {
		case o.ShipDate != "" && o.ShipDate <= c:
			io.Status, io.ShipDate = "출하 완료", o.ShipDate
		case o.CustomerID == "NONE":
			io.Status = "대리점(출하 기록 없음)"
		default:
			io.Status, io.EstDue = "진행", EstDue(at, o)
		}
		in.Orders = append(in.Orders, io)
	}
	for _, o := range b.Orders {
		if o.ShipDate != "" && o.ShipDate <= c && daysBetween(o.ShipDate, c) < 7 {
			in.Shipped7++
		}
	}
	return in
}

// BacktestRow는 한 달의 추정 납품량과 실제 출하량이다.
type BacktestRow struct {
	Month     string `json:"month"`
	Estimated int64  `json:"estimated"`
	Actual    int64  `json:"actual"`
}

// Backtest는 정답 비교다. 잘라 낸 날 뒤의 실제 출하는 이 보기에서만 쓴다(판단에 쓰지 않음).
type Backtest struct {
	Cutoff    string                   `json:"cutoff"`
	Available bool                     `json:"available"`
	Note      string                   `json:"note"`
	Rows      map[string][]BacktestRow `json:"rows"`
	HitRate   map[string]float64       `json:"hit_rate"` // 실제 출하 달 = 추정 납기 달인 주문 비율
	Orders    map[string]int           `json:"orders"`
}

// Backtest는 진행 주문의 추정 납기(기준일 기록만으로)와 실제 출하(기준일 뒤 기록)를 비교한다.
func (s *State) Backtest(b *Base) Backtest {
	at := b.At(s.Scenario.SimDate)
	c := at.Cutoff
	bt := Backtest{Cutoff: c, Rows: map[string][]BacktestRow{}, HitRate: map[string]float64{}, Orders: map[string]int{}}
	if c >= DataEnd {
		bt.Note = "자료 마지막 날(" + DataEnd + ") 기준이라 비교할 실제 출하가 없습니다. 더 이른 기준일을 선택해 주세요."
		return bt
	}
	bt.Available = true
	bt.Note = "검증 전용: 기준일 뒤 실제 출하는 비교에만 사용합니다(판단에는 사용하지 않음)."
	agg := map[string]map[string]*BacktestRow{}
	hits := map[string]int{}
	for _, o := range b.OpenOrders(c) {
		p := o.ProductID
		if agg[p] == nil {
			agg[p] = map[string]*BacktestRow{}
		}
		row := func(m string) *BacktestRow {
			if agg[p][m] == nil {
				agg[p][m] = &BacktestRow{Month: m}
			}
			return agg[p][m]
		}
		est := EstDue(at, o)
		if est != "" {
			row(est[:7]).Estimated += o.Qty
		}
		if o.ShipDate != "" {
			row(o.ShipDate[:7]).Actual += o.Qty
			if est != "" && est[:7] == o.ShipDate[:7] {
				hits[p]++
			}
		}
		bt.Orders[p]++
	}
	for p, m := range agg {
		var rows []BacktestRow
		for _, r := range m {
			rows = append(rows, *r)
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].Month < rows[j].Month })
		bt.Rows[p] = rows
		if bt.Orders[p] > 0 {
			bt.HitRate[p] = float64(hits[p]) / float64(bt.Orders[p]) * 100
		}
	}
	return bt
}

// daysBetween은 a에서 b까지 일수다(b가 뒤면 양수).
func daysBetween(a, b string) int {
	return int(mustDate(b).Sub(mustDate(a)).Hours() / 24)
}

// nextMonths는 잘라 낸 날 다음 날이 속한 달부터 n개월이다.
func nextMonths(c string, n int) []string {
	t := mustDate(c).AddDate(0, 0, 1)
	first := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	out := make([]string, n)
	for i := range out {
		out[i] = first.AddDate(0, i, 0).Format("2006-01")
	}
	return out
}
