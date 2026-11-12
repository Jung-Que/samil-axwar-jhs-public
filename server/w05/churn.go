package w05

import "fmt"

// R12 이탈 경보 · R13 이탈 의심 고객 능력 반환 금지(이슈 #33, 명세 01_도메인_지식.md §9).
//
// W05 단초: "기존 고객 C-101의 수주 감소와 정상 출하 보고는 서로 다른 시점 지표다." 재무 메모(sales_memo_cv)는 출하 기준으로
// 정상이라 보고하고, 영업 회의록(sales_meeting_notes_cv)은 신규 발주 감소를 "단정 금지"로 적었다. 그래서 수주와 출하를 나눠 본다.
// 기준일 재현과 같이 잘라 낸 날까지 끝난 달(닫힌 달)만 쓴다. 임계값은 어떤 출처도 정하지 않은 설계값이다(가정 A-23).

const (
	ChurnShareMin    = 10.0  // 대상: 매출 비중(m_customer_cv.revenue_share_pct) 10% 이상
	ChurnRollingMax  = 0.80  // AT_RISK: 3개월 수주/출하 < 0.8
	ChurnYoYMax      = -15.0 // AT_RISK: 3개월 수주 YoY < −15%
	ChurnWatchSingle = 0.85  // WATCH: 단월 수주/출하 < 0.85가 2개월 연속
	churnHistory     = 6

	ChurnOK     = "정상"
	ChurnWatch  = "WATCH"
	ChurnAtRisk = "AT_RISK"
)

// ChurnMonth는 고객의 한 달 수주·출하(금액)와 경보 상태다.
type ChurnMonth struct {
	Month    string  `json:"month"`
	Booking  int64   `json:"booking_krw"`
	Shipment int64   `json:"shipment_krw"`
	Single   float64 `json:"single_ratio"`  // 단월 수주/출하
	Rolling  float64 `json:"rolling_ratio"` // 3개월 수주/출하
	YoY      float64 `json:"yoy_pct"`       // 3개월 수주 YoY(%)
	HasYoY   bool    `json:"has_yoy"`       // 15개월치 기록이 있어 YoY를 잴 수 있음
	Status   string  `json:"status"`
}

// ChurnSignal은 고객 한 곳의 이탈 신호다. Month는 판단한 달(잘라 낸 날의 마지막 닫힌 달)이다.
type ChurnSignal struct {
	CustomerID   string           `json:"customer_id"`
	Name         string           `json:"customer_name"`
	SharePct     float64          `json:"revenue_share_pct"`
	Month        string           `json:"month"`
	Status       string           `json:"status"`
	Since        string           `json:"since,omitempty"` // 지금 경보가 이어진 첫 달
	Single       float64          `json:"single_ratio"`
	Rolling      float64          `json:"rolling_ratio"`
	YoY          float64          `json:"yoy_pct"`
	HasYoY       bool             `json:"has_yoy"`
	OpenOrders   int              `json:"open_orders"`    // 진행 주문(R13: AT_RISK면 '검토', 보호 유지)
	OpenQty      map[string]int64 `json:"open_qty"`       // 제품군별
	T12BookedQty map[string]int64 `json:"t12_booked_qty"` // 최근 12개월 수주 수량(CR-09 반환 상한)
	History      []ChurnMonth     `json:"history"`
}

func ratio(n, d int64) float64 {
	if d <= 0 {
		return 0
	}
	return float64(n) / float64(d)
}

// churnSignals는 잘라 낸 날 기준 대상 고객의 이탈 신호를 만든다(At에서 한 번 계산해 캐시).
func (b *Base) churnSignals(a *AsOfData) []ChurnSignal {
	last := lastClosedMonth(a.Cutoff)
	first := openFrom[:7] // 수주 기록 첫 달
	open := b.OpenOrders(a.Cutoff)
	var out []ChurnSignal
	for _, cu := range b.Customers {
		if cu.SharePct < ChurnShareMin {
			continue
		}
		bk, sh := b.booked[cu.ID], b.shipped[cu.ID]
		month := func(m string) ChurnMonth {
			cm := ChurnMonth{Month: m, Booking: bk[m], Shipment: sh[m], Single: ratio(bk[m], sh[m]), Status: ChurnOK}
			r3 := monthsBack(m, 3)
			if r3[2] >= first {
				var b3, s3 int64
				for _, x := range r3 {
					b3 += bk[x]
					s3 += sh[x]
				}
				cm.Rolling = ratio(b3, s3)
				p3 := monthsBack(monthsBack(m, 13)[12], 3) // m−12, m−13, m−14
				if p3[2] >= first {
					var bp int64
					for _, x := range p3 {
						bp += bk[x]
					}
					if bp > 0 {
						cm.YoY, cm.HasYoY = (float64(b3)-float64(bp))/float64(bp)*100, true
					}
				}
			}
			return cm
		}
		months := monthsBack(last, churnHistory+1) // 앞 달 단월 비율(WATCH 2개월 연속)을 보려고 한 달 더
		var hist []ChurnMonth
		for i := len(months) - 1; i >= 0; i-- {
			if months[i] < first {
				continue
			}
			cm := month(months[i])
			switch {
			case cm.HasYoY && cm.Rolling > 0 && cm.Rolling < ChurnRollingMax && cm.YoY < ChurnYoYMax:
				cm.Status = ChurnAtRisk
			case len(hist) > 0 && cm.Shipment > 0 && cm.Single < ChurnWatchSingle && hist[len(hist)-1].Shipment > 0 && hist[len(hist)-1].Single < ChurnWatchSingle:
				cm.Status = ChurnWatch
			}
			hist = append(hist, cm)
		}
		if len(hist) > churnHistory {
			hist = hist[len(hist)-churnHistory:]
		}
		sg := ChurnSignal{CustomerID: cu.ID, Name: cu.Name, SharePct: cu.SharePct, Month: last, Status: ChurnOK, History: hist,
			OpenQty: map[string]int64{}, T12BookedQty: map[string]int64{}}
		if n := len(hist); n > 0 {
			cur := hist[n-1]
			sg.Status, sg.Single, sg.Rolling, sg.YoY, sg.HasYoY = cur.Status, cur.Single, cur.Rolling, cur.YoY, cur.HasYoY
			if sg.Status != ChurnOK {
				for i := n - 1; i >= 0 && hist[i].Status != ChurnOK; i-- {
					sg.Since = hist[i].Month
				}
			}
		}
		for _, o := range open {
			if o.CustomerID == cu.ID {
				sg.OpenOrders++
				sg.OpenQty[o.ProductID] += o.Qty
			}
		}
		for _, p := range []string{ProductBasic, ProductDerma} {
			for _, m := range monthsBack(last, 12) {
				sg.T12BookedQty[p] += b.bookedQty[cu.ID+"|"+p][m]
			}
		}
		out = append(out, sg)
	}
	return out
}

// atRisk는 AT_RISK 고객 중 첫 곳이다(원본에서는 C-101 2026-06 한 곳뿐이라 확인 요청 CR-09도 하나다).
func atRisk(sigs []ChurnSignal) *ChurnSignal {
	for i := range sigs {
		if sigs[i].Status == ChurnAtRisk {
			return &sigs[i]
		}
	}
	return nil
}

// cr09Question은 R13 영업 임원 확인 요청(CR-09) 질문이다.
func cr09Question(sg ChurnSignal) string {
	return fmt.Sprintf("%s 수주 감소가 계약·예측 수정으로 확인되는가? 확인되면 C-201에 돌릴 연간 수량(최근 12개월 수주 PL-01 %s · PL-02 %s EA 이내). 근거: %s",
		sg.CustomerID, Commas(sg.T12BookedQty[ProductBasic]), Commas(sg.T12BookedQty[ProductDerma]), churnText(sg))
}

func churnText(sg ChurnSignal) string {
	yoy := "YoY 계산 불가(기록 15개월 미만)"
	if sg.HasYoY {
		yoy = fmt.Sprintf("3개월 수주 YoY %.1f%%", sg.YoY)
	}
	return fmt.Sprintf("%s(매출 비중 %.0f%%) %s: 단월 수주/출하 %.3f, 3개월 %.3f, %s", sg.CustomerID, sg.SharePct, sg.Month, sg.Single, sg.Rolling, yoy)
}
