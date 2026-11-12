package w05

import "fmt"

// 시뮬레이션 3(이슈 #41): 원본 기록으로 굴러가는 시계.
// 시계(E0)를 옮기면 그 기간의 원본 기록(신규 수주·출하·Masil 발주·이탈 신호 변화)을 사건 결과로 남기고,
// 세션 기준일 뒤 신규 수주를 C-201 여유에서 뺀다(사용자 결정 11/13 "신규 수주도 적용").
//
// 빼는 양의 기본은 진행 주문 순증분(신규 수주 − 그 기간 출하로 끝난 물량)이다. 능력 스냅샷 가동률에는 이미 주문 흐름이
// 들어 있어(A-06) 총량을 빼면 이중 차감이 된다(1개월 PL-02 신규 수주 약 49만 EA 대 L-02 연 여유 58,484 EA).
// 줄어든 만큼은 돌려주지 않는다(보수적, R13과 같은 원칙). 총량 기준은 시뮬레이션 세션의 모의 가정으로만 고른다.

// 신규 수주 반영 기준.
const (
	HistNet   = "net"   // 진행 주문 순증분(기본)
	HistGross = "gross" // 기간 신규 수주 총량(모의 가정)
)

// HistWindow는 (From, To] 기간의 원본 기록 요약이다. 날짜는 데이터 끝에서 자른 값이다.
type HistWindow struct {
	From, To     string
	NewOrders    int
	NewQty       map[string]int64
	AgencyOrders int
	Shipments    int
	SPOs         int
	SPOQty       map[string]int64
}

func (b *Base) histWindow(from, to string) HistWindow {
	w := HistWindow{From: from, To: to, NewQty: map[string]int64{}, SPOQty: map[string]int64{}}
	for _, o := range b.Orders {
		if o.OrderDate > from && o.OrderDate <= to {
			if o.CustomerID == "NONE" {
				w.AgencyOrders++
			} else {
				w.NewOrders++
				w.NewQty[o.ProductID] += o.Qty
			}
		}
		if o.ShipDate != "" && o.ShipDate > from && o.ShipDate <= to {
			w.Shipments++
		}
	}
	for _, s := range b.SPOs {
		if s.OrderDate > from && s.OrderDate <= to {
			w.SPOs++
			w.SPOQty[s.ProductID] += s.Qty
		}
	}
	return w
}

// histOrders는 세션 기준일 뒤 업무일까지의 신규 수주 중 C-201 여유에서 뺄 양이다(PL-01·PL-02).
func (b *Base) histOrders(asOf, sim, basis string) map[string]int64 {
	c0, c1 := CutoffOf(asOf), CutoffOf(sim)
	if c1 <= c0 {
		return nil
	}
	out := map[string]int64{}
	if basis == HistGross {
		w := b.histWindow(c0, c1)
		for _, p := range []string{ProductBasic, ProductDerma} {
			if q := w.NewQty[p]; q > 0 {
				out[p] = q
			}
		}
	} else {
		a0, a1 := b.At(c0).Commit.ByProduct, b.At(c1).Commit.ByProduct
		for _, p := range []string{ProductBasic, ProductDerma} {
			if d := a1[p] - a0[p]; d > 0 {
				out[p] = d
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func histBasisLabel(basis string) string {
	if basis == HistGross {
		return "신규 수주 총량(모의 가정)"
	}
	return "진행 주문 순증분(신규 수주 − 그 기간 출하 완료, 줄어든 만큼은 돌려주지 않음)"
}

// clockNotes는 시계를 from → to로 옮길 때 원본 기록과 이탈 신호 변화를 문장으로 만든다.
func (b *Base) clockNotes(from, to string) []string {
	c0, c1 := CutoffOf(from), CutoffOf(to)
	if c1 <= c0 {
		if to > DataEnd {
			return []string{fmt.Sprintf("원본 기록은 %s까지라 그 뒤로는 새 기록이 들어오지 않는다", DataEnd)}
		}
		return nil
	}
	w := b.histWindow(c0, c1)
	notes := []string{fmt.Sprintf("원본 기록(%s 다음 날 ~ %s): 신규 B2B 수주 %s건(PL-01 %s · PL-02 %s EA), 대리점 수주 %s건, 출하 %s건, Masil 발주 %s건(PL-01 %s · PL-02 %s EA)",
		c0, c1, Commas(int64(w.NewOrders)), Commas(w.NewQty[ProductBasic]), Commas(w.NewQty[ProductDerma]), Commas(int64(w.AgencyOrders)),
		Commas(int64(w.Shipments)), Commas(int64(w.SPOs)), Commas(w.SPOQty[ProductBasic]), Commas(w.SPOQty[ProductDerma]))}
	before := map[string]string{}
	for _, sg := range b.At(c0).Churn {
		before[sg.CustomerID] = sg.Status
	}
	for _, sg := range b.At(c1).Churn {
		if prev := before[sg.CustomerID]; prev != "" && prev != sg.Status {
			notes = append(notes, fmt.Sprintf("이탈 신호 %s: %s → %s(%s 기준, R12)", sg.CustomerID, prev, sg.Status, sg.Month))
		}
	}
	return notes
}
