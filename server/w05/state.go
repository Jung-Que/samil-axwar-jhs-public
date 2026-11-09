package w05

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// 사건 종류(명세 §5). E6(Masil 수요 변경)은 설계 단계라 없다.
const (
	EvClock      = "E0_CLOCK"
	EvRFP        = "E1_RFP"
	EvDecision   = "E2_DECISION"
	EvReply      = "E3_REPLY"
	EvBuyer      = "E4_BUYER_REPLY"
	EvNewOrder   = "E5_NEW_ORDER"
	EvSubmit     = "E7_SUBMIT_PROPOSAL"
	EvForApprove = "E8_SUBMIT_APPROVAL"
)

// 입찰 상태(명세 §3.1).
const (
	BidReviewing   = "검토중"
	BidSubmitted   = "상신"
	BidApproved    = "승인"
	BidConditional = "조건부 승인"
	BidOnHold      = "보류"
	BidAwaiting    = "회신 대기"
	BidRereview    = "재검토"
	BidWon         = "수주"
	BidLost        = "실주"
)

// Event는 사건 로그 한 줄이다. 저장소에는 이 구조를 JSON으로 넣는다.
type Event struct {
	Seq     int             `json:"seq"`
	Type    string          `json:"type"`
	Actor   string          `json:"actor_role"`
	SimDate string          `json:"sim_date"`
	Payload json.RawMessage `json:"payload"`
	AiCall  *AiCall         `json:"ai_call,omitempty"`
	At      string          `json:"created_at,omitempty"`
}

// Bid는 입찰이다.
type Bid struct {
	ID            string   `json:"bid_id"`
	State         string   `json:"state"`
	Received      string   `json:"received_date"`
	Deadline      string   `json:"deadline"`
	Currency      string   `json:"currency"`
	PenaltyRate   float64  `json:"penalty_rate_monthly"`
	RecallBearer  string   `json:"recall_bearer"`
	Version       int      `json:"version"`
	ApprovalRoute []string `json:"approval_route"`
}

// Condition은 제안 품목에 붙는 조건이다(R14 점검표 코드 + 설명).
type Condition struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

// MonthQty는 제안 일정 한 달이다.
type MonthQty struct {
	Month       string   `json:"month"`
	Qty         int64    `json:"qty"`
	Basis       []string `json:"basis"`
	Conditional bool     `json:"conditional"`
}

// BidLine은 입찰 품목(제안 내용)이다.
type BidLine struct {
	ProductID     string      `json:"product_id"`
	RequestedQty  int64       `json:"requested_annual_qty"`
	UnitPriceUSD  float64     `json:"unit_price_usd"`
	PriceCut      float64     `json:"price_cut"`
	QtyStatus     string      `json:"qty_status"`
	Schedule      []MonthQty  `json:"schedule"`
	ProposedTotal int64       `json:"proposed_total"`
	Conditions    []Condition `json:"conditions"`
	Note          string      `json:"note"`
}

// ConfirmRequest는 확인 요청이다(명세 §5 CR-01~08).
type ConfirmRequest struct {
	ID       string         `json:"request_id"`
	ToRole   string         `json:"to_role"`
	Question string         `json:"question"`
	Blocks   []string       `json:"blocks"`
	Due      string         `json:"due_date"`
	State    string         `json:"state"` // 요청 | 반영 | 기한 초과
	Answer   map[string]any `json:"answer,omitempty"`
	OpenedAt int            `json:"opened_seq"`
	Mock     bool           `json:"mock_reply"`
}

// Task는 사람에게 넘어가는 작업이다.
type Task struct {
	ID       string `json:"task_id"`
	Owner    string `json:"owner_role"`
	Title    string `json:"title"`
	Due      string `json:"due_date,omitempty"`
	State    string `json:"state"` // 열림 | 완료 | 취소
	CauseSeq int    `json:"cause_seq"`
}

// Decision은 승인권자의 결정이다(R14·R15).
type Decision struct {
	ID            string            `json:"decision_id"`
	Approver      string            `json:"approver_role"`
	Participation string            `json:"participation"`
	Options       map[string]string `json:"options"`
	Conditions    []string          `json:"conditions"`
	PriceFloor    *float64          `json:"price_floor"`
	Note          string            `json:"note"`
	DecidedAt     string            `json:"decided_at"`
	EvalVersion   int               `json:"eval_version"`
	ContentHash   string            `json:"content_hash"`
	Freshness     string            `json:"freshness"` // 최신 | 낡음
	Seq           int               `json:"seq"`
}

// Reservation은 제출된 C-201 제안의 가예약(soft)이다(R2).
type Reservation struct {
	ProductID string `json:"product_id"`
	Month     string `json:"month"`
	Qty       int64  `json:"qty"`
	Basis     string `json:"basis"`
	State     string `json:"state"` // 가예약
}

// LogEntry는 사건과 그 결과다.
type LogEntry struct {
	Event   Event    `json:"event"`
	Changes []string `json:"changes"`
}

// State는 사건을 처음부터 적용해 만든 현재 상태다.
type State struct {
	Scenario     Scenario          `json:"scenario"`
	Bid          *Bid              `json:"bid"`
	Lines        []*BidLine        `json:"lines"`
	Requests     []*ConfirmRequest `json:"requests"`
	Tasks        []*Task           `json:"tasks"`
	Decisions    []*Decision       `json:"decisions"`
	Reservations []Reservation     `json:"reservations"`
	NewOrders    []map[string]any  `json:"new_orders"`
	Log          []LogEntry        `json:"log"`
	EvalVersion  int               `json:"eval_version"`
	Eval         *Evaluation       `json:"evaluation"`
	PrevEval     *Evaluation       `json:"-"`
	LastBlocks   []string          `json:"last_blocks"`
	submittedSig string
}

// NewState는 사건이 하나도 없는 상태다(기초 원장과 가정만 있음).
func NewState(b *Base) *State {
	s := &State{Scenario: DefaultScenario()}
	s.Eval = Evaluate(b, s.Scenario)
	return s
}

// Apply는 사건 하나를 적용한다. 오류면 상태를 바꾸지 않는다(호출자가 복사본에 적용).
func (s *State) Apply(b *Base, e Event) error {
	var ch []string
	note := func(f string, a ...any) { ch = append(ch, fmt.Sprintf(f, a...)) }
	if e.SimDate == "" {
		e.SimDate = s.Scenario.SimDate
	}

	switch e.Type {
	case EvClock:
		var p struct {
			SimDate string `json:"sim_date"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		if _, ok := parseDate(p.SimDate); !ok || p.SimDate < s.Scenario.SimDate {
			return fmt.Errorf("시계는 앞으로만 간다: %q (지금 %s)", p.SimDate, s.Scenario.SimDate)
		}
		s.Scenario.SimDate = p.SimDate
		e.SimDate = p.SimDate
		note("업무 날짜 → %s", p.SimDate)
		for _, r := range s.Requests {
			if r.State == "요청" && r.Due < p.SimDate {
				r.State = "기한 초과"
				note("%s 기한 초과(%s) → 재요청 작업", r.ID, r.Due)
				s.addTask("영업", fmt.Sprintf("재요청: %s(%s) %s", r.ID, r.ToRole, r.Question), addBusinessDays(p.SimDate, 2), e.Seq)
			}
		}

	case EvRFP:
		if s.Bid != nil {
			return fmt.Errorf("이미 접수된 입찰이 있다(초기화 후 다시)")
		}
		var p RFPFields
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		if err := validateItems(p.Items); err != nil {
			return err
		}
		if p.DeadlineWeeks <= 0 {
			p.DeadlineWeeks = 3
		}
		recv := e.SimDate
		s.Scenario.Items = p.Items
		s.Bid = &Bid{ID: BidID, State: BidReviewing, Received: recv, Deadline: fmtDate(mustDate(recv).AddDate(0, 0, 7*p.DeadlineWeeks)),
			Currency: p.Currency, PenaltyRate: p.PenaltyRate, RecallBearer: p.RecallBearer, Version: 1}
		note("입찰 %s 접수 → 검토중(마감 %s, A-03)", BidID, s.Bid.Deadline)
		s.openInitialRequests(b, e.Seq)
		note("확인 요청 %d건 생성", len(s.Requests))
		s.addTask("영업", "상신안 검토: 엔진 제안안·근거·미확인 확인 후 상신", s.Bid.Deadline, e.Seq)

	case EvForApprove:
		if s.Bid == nil || (s.Bid.State != BidReviewing && s.Bid.State != BidRereview) {
			return fmt.Errorf("상신은 검토중·재검토 상태에서만 된다(지금 %s)", s.bidState())
		}
		ev := Evaluate(b, s.Scenario)
		if probs := proposalChecks(ev, s.Scenario); len(probs) > 0 {
			return fmt.Errorf("상신 전 검사 실패: %s", strings.Join(probs, "; "))
		}
		if s.Bid.State == BidRereview {
			s.Bid.Version++
		}
		s.Bid.State = BidSubmitted
		s.Bid.ApprovalRoute = []string{"CEO/이사회"}
		note("상신(제안 v%d) → 승인 라우팅: 입찰 참여·투자 = CEO/이사회(R14)", s.Bid.Version)
		s.closeTasks("상신안 검토", "재상신")
		s.addTask("CEO/이사회", fmt.Sprintf("승인 검토: C-201 참여·OPT-01/02 투자(제안 v%d)", s.Bid.Version), BoardDate, e.Seq)

	case EvDecision:
		if s.Bid == nil || s.Bid.State != BidSubmitted {
			return fmt.Errorf("결정은 상신 상태에서만 받는다(지금 %s)", s.bidState())
		}
		var p DecisionFields
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		if err := p.validate(); err != nil {
			return err
		}
		if p.DecidedAt == "" {
			p.DecidedAt = e.SimDate
			if p.DecidedAt < BoardDate { // 승인권자(CEO/이사회) 결정은 이사회 날부터(A-09)
				p.DecidedAt = BoardDate
			}
		}
		var floor *float64
		if p.PriceFloorPct != nil {
			f := *p.PriceFloorPct / 100
			floor = &f
		}
		for id, st := range p.Options {
			switch st {
			case OptApproved, OptDeferred, OptRejected:
				s.Scenario.Options[id] = OptionDecision{Status: st, DecisionDate: p.DecidedAt}
				note("%s → %s", id, st)
			case "", "언급없음":
			default:
				return fmt.Errorf("%s 결정 값 오류: %q", id, st)
			}
		}
		for _, d := range s.Decisions {
			d.Freshness = "낡음"
		}
		d := &Decision{ID: fmt.Sprintf("D-%02d", len(s.Decisions)+1), Approver: "CEO/이사회", Participation: p.Participation,
			Options: p.Options, Conditions: p.Conditions, PriceFloor: floor, Note: p.Note, DecidedAt: p.DecidedAt, Freshness: "최신", Seq: e.Seq}
		s.Decisions = append(s.Decisions, d)
		s.closeTasks("승인 검토")
		switch p.Participation {
		case "거절":
			s.Bid.State = BidOnHold
			note("참여 거절 → 입찰 보류(확인 요청은 열린 채 유지)")
			s.addTask("영업", "참여 보류 통지와 후속 확인 유지", addBusinessDays(e.SimDate, 2), e.Seq)
		default:
			s.Bid.State = BidConditional
			if p.Participation == "승인" {
				s.Bid.State = BidApproved
			}
			note("입찰 %s, 조건 %d개 → 제출 전 점검표", s.Bid.State, len(p.Conditions))
			s.addTask("영업", "제안서 작성: 승인 조건 반영", s.Bid.Deadline, e.Seq)
			s.addTask("생산관리", "생산계획 잠정 반영 요청(L-02·F2 신규 라인, 확정 아님)", addBusinessDays(e.SimDate, 5), e.Seq)
			s.addTask("그룹 재무", fundingTask(p.Options), addBusinessDays(e.SimDate, 5), e.Seq)
			if p.Options["OPT-01"] == OptRejected {
				s.addTask("영업", "바이어 범위 축소 협상: 더마는 OPT-02 범위(연 600,000 EA)로", s.Bid.Deadline, e.Seq)
			}
		}

	case EvReply:
		var p struct {
			RequestID string         `json:"request_id"`
			Answer    map[string]any `json:"answer"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		r := s.request(p.RequestID)
		if r == nil || r.State == "반영" {
			return fmt.Errorf("열린 확인 요청이 아니다: %s", p.RequestID)
		}
		if err := s.applyAnswer(r.ID, p.Answer); err != nil {
			return err
		}
		r.State, r.Answer, r.Mock = "반영", p.Answer, true
		note("%s 회신 반영(%s)", r.ID, r.ToRole)
		s.closeTasks("재요청: " + r.ID)

	case EvBuyer:
		if s.Bid == nil {
			return fmt.Errorf("접수된 입찰이 없다")
		}
		var p BuyerReply
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		if err := p.validate(); err != nil {
			return err
		}
		s.applyBuyer(p, note)
		if p.Final == "수주" || p.Final == "실주" {
			s.Bid.State = p.Final
			note("입찰 종결: %s", p.Final)
		}

	case EvNewOrder:
		var p struct {
			ProductID  string `json:"product_id"`
			Qty        int64  `json:"qty"`
			CustomerID string `json:"customer_id"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		if (p.ProductID != ProductBasic && p.ProductID != ProductDerma) || p.Qty <= 0 {
			return fmt.Errorf("신규 수주는 PL-01·PL-02, 수량 > 0")
		}
		if s.Scenario.NewOrders == nil {
			s.Scenario.NewOrders = map[string]int64{}
		}
		s.Scenario.NewOrders[p.ProductID] += p.Qty
		s.NewOrders = append(s.NewOrders, map[string]any{"product_id": p.ProductID, "qty": p.Qty, "customer_id": p.CustomerID, "seq": e.Seq})
		note("스냅샷 이후 신규 수주 %s %s EA → 기본 여유에서 차감(R1)", p.ProductID, Commas(p.Qty))

	case EvSubmit:
		if s.Bid == nil || (s.Bid.State != BidApproved && s.Bid.State != BidConditional) {
			return fmt.Errorf("제출은 승인·조건부 승인 상태에서만 된다(지금 %s)", s.bidState())
		}
		ev := Evaluate(b, s.Scenario)
		lines := buildLines(ev, s.Scenario, s.Bid)
		if fails := s.checklist(ev, lines); len(fails) > 0 {
			// 차단도 기록으로 남긴다(명세 §5 E7). 상태 전이는 없다.
			s.LastBlocks = fails
			note("제출 차단(점검표 실패): %s", strings.Join(fails, "; "))
			s.Log = append(s.Log, LogEntry{Event: e, Changes: ch})
			return nil
		}
		s.LastBlocks = nil
		s.Bid.State = BidAwaiting
		s.Reservations = nil
		for _, l := range lines {
			for _, m := range l.Schedule {
				if m.Qty > 0 && l.QtyStatus != "미확정" {
					s.Reservations = append(s.Reservations, Reservation{ProductID: l.ProductID, Month: m.Month, Qty: m.Qty, Basis: strings.Join(m.Basis, "+"), State: "가예약"})
				}
			}
		}
		s.submittedSig = contentHash(lines, s.Scenario)
		note("제안 제출(점검표 통과) → 회신 대기, C-201 가예약 %d건(확정 약속을 밀지 않음, R2)", len(s.Reservations))
		s.closeTasks("제안서 작성")
		s.addTask("영업", "바이어 회신 추적(초도일·분할·페널티 기준·생산지)", addBusinessDays(e.SimDate, 5), e.Seq)

	default:
		return fmt.Errorf("모르는 사건 종류: %s", e.Type)
	}

	s.reevaluate(b, e.Seq, note)
	s.Log = append(s.Log, LogEntry{Event: e, Changes: ch})
	return nil
}

// reevaluate는 사건 뒤 평가를 새로 만들고 제안 품목·결정 신선도·충돌 요청을 맞춘다.
func (s *State) reevaluate(b *Base, seq int, note func(string, ...any)) {
	s.PrevEval = s.Eval
	s.Eval = Evaluate(b, s.Scenario)
	s.EvalVersion++
	if s.Bid == nil {
		return
	}
	s.Lines = buildLines(s.Eval, s.Scenario, s.Bid)
	sig := contentHash(s.Lines, s.Scenario)
	if s.submittedSig != "" && sig == s.submittedSig {
		for _, l := range s.Lines {
			if l.QtyStatus != "미확정" {
				l.QtyStatus = "제안됨"
			}
		}
	}
	// R15: 결정은 승인 당시 내용에 묶인다. 첫 평가에서 결속하고, 내용이 바뀌면 낡음.
	for _, d := range s.Decisions {
		if d.Freshness != "최신" {
			continue
		}
		if d.ContentHash == "" {
			d.ContentHash, d.EvalVersion = sig, s.EvalVersion
			continue
		}
		if d.ContentHash != sig {
			d.Freshness = "낡음"
			note("결정 %s 낡음: 승인 뒤 제안 내용이 바뀜(R15) → 재상신 필요", d.ID)
			if s.Bid.State == BidApproved || s.Bid.State == BidConditional || s.Bid.State == BidAwaiting {
				s.Bid.State = BidRereview
				s.addTask("영업", "재상신: 바뀐 조건으로 승인 다시 받기", addBusinessDays(s.Scenario.SimDate, 2), seq)
			}
		}
	}
	// R2: 바이어 첫 달 요구가 공급을 넘으면 Masil 보호분 침범 대신 Masil SCM 확인 요청
	if s.Eval.InitialGap > 0 && s.request("CR-08") == nil {
		s.Requests = append(s.Requests, &ConfirmRequest{ID: "CR-08", ToRole: "Masil SCM",
			Question: fmt.Sprintf("Masil 더마 발주(88,000 EA) 중 최대 %s EA의 입고를 늦출 수 있는가(C-201 초도 %s)", Commas(s.Eval.InitialGap), s.Scenario.DeliveryStart),
			Blocks:   []string{"R2", "PL-02 첫 달 수량"}, Due: addBusinessDays(s.Scenario.SimDate, 3), State: "요청", OpenedAt: seq})
		note("첫 달 요구가 공급보다 %s EA 많음 → Masil 보호분 침범 차단, CR-08(Masil SCM) 생성", Commas(s.Eval.InitialGap))
	}
}

func (s *State) bidState() string {
	if s.Bid == nil {
		return "접수 전"
	}
	return s.Bid.State
}

// openInitialRequests는 E1에서 결정 모델이 정한 확인 요청을 만든다(명세 §5 표).
func (s *State) openInitialRequests(b *Base, seq int) {
	recv, dl := s.Bid.Received, s.Bid.Deadline
	ev := Evaluate(b, s.Scenario)
	add := func(id, to, q, due string, blocks ...string) {
		s.Requests = append(s.Requests, &ConfirmRequest{ID: id, ToRole: to, Question: q, Blocks: blocks, Due: due, State: "요청", OpenedAt: seq})
	}
	add("CR-01", "생산관리", "capacity_qty의 기간 단위(연간·월간)", addBusinessDays(recv, 5), "R4", "전체 여유")
	add("CR-02", "생산관리", "PL-01 600만 EA를 소화할 수단(증산·외주·전환)과 연간 추가 가능 수량", addBusinessDays(recv, 5), "R6", "PL-01 수량")
	for _, l := range ev.Lines {
		if l.MasilDeficit > 0 {
			add("CR-03", "생산관리", fmt.Sprintf("Masil SPO(%s %s EA)가 %s 계획(가동률 %.0f%%)에 들어 있는가", l.ProductID, Commas(l.MasilProtected), l.LineID, l.UtilPct), addBusinessDays(recv, 5), "R1", "A-07")
			break
		}
	}
	add("CR-04", "바이어", "납품 개시일·분할 납품 허용·계약 기간", addBusinessDays(dl, -5), "R7", "R8", "A-04", "A-05")
	add("CR-05", "바이어", "납기 지연 페널티의 기준액(지연분·계약 총액)과 상한", addBusinessDays(dl, -5), "R8", "A-13")
	add("CR-06", "바이어", "F2 해외공장 생산지(OPT-02) 수용 여부", addBusinessDays(dl, -5), "R3", "OPT-02")
	add("CR-07", "구매·품질", "M-B 재계약 가능 여부와 처방 재검증 기간(주)", addBusinessDays(recv, 10), "R3", "OPT-02", "A-08")
}

// applyAnswer는 내부 부서 회신(E3)을 가정·시나리오에 반영한다.
func (s *State) applyAnswer(id string, a map[string]any) error {
	switch id {
	case "CR-01":
		v, _ := a["capacity_period"].(string)
		if v != "annual" && v != "monthly" {
			return fmt.Errorf("CR-01 회신: capacity_period는 annual·monthly")
		}
		s.Scenario.CapacityPeriod, s.Scenario.UnitConfirmed = v, true
	case "CR-02":
		s.Scenario.PL01Confirmed = true
		s.Scenario.PL01ExtraAnnual = int64(num(a["extra_annual"]))
	case "CR-03":
		v, ok := a["masil_in_util"].(bool)
		if !ok {
			return fmt.Errorf("CR-03 회신: masil_in_util(true/false) 필요")
		}
		s.Scenario.MasilInUtil = v
	case "CR-07":
		v, ok := a["mb_ready"].(bool)
		if !ok {
			return fmt.Errorf("CR-07 회신: mb_ready(true/false) 필요")
		}
		s.Scenario.MBReady = v
		if w := int(num(a["validation_weeks"])); w > 0 {
			s.Scenario.ValidationWeeks = w
		}
	case "CR-08":
		q := int64(num(a["released_qty"]))
		if q < 0 || q > 88_000 {
			return fmt.Errorf("CR-08 회신: released_qty는 0~88,000")
		}
		s.Scenario.MasilReleased = q
	default:
		return fmt.Errorf("%s는 바이어 회신(E4)으로 반영한다", id)
	}
	return nil
}

// DecisionFields는 승인권자 회신을 구조화한 필드다(A1 출력 또는 폼 입력).
type DecisionFields struct {
	Participation string            `json:"participation"` // 승인 | 조건부 | 거절
	Options       map[string]string `json:"options"`       // OPT-01·OPT-02 → 승인 | 보류 | 거절 | 언급없음
	Conditions    []string          `json:"conditions"`
	PriceFloorPct *float64          `json:"price_floor_pct"` // 허용 단가 인하 하한(%), 예: -10
	Note          string            `json:"note"`
	DecidedAt     string            `json:"decided_at,omitempty"`
}

// ConditionCodes는 승인 조건 코드와 뜻이다(명세 §4 R14).
var ConditionCodes = map[string]string{
	"COND_PL01_UNCONFIRMED": "PL-01은 생산관리 확인 전 수량 미확정",
	"COND_NO_PRE_QUAL":      "적격일 전 증설·전환 능력 산입 금지",
	"COND_OPT02_F2":         "OPT-02 물량은 바이어 생산지 수용 전 조건부",
	"COND_OPT01_STAGED":     "OPT-01 의존 물량은 OPT-01 승인 전 조건부",
	"COND_MASIL_PROTECTED":  "Masil 보호분 침범 금지",
}

func (p DecisionFields) validate() error {
	if p.Participation != "승인" && p.Participation != "조건부" && p.Participation != "거절" {
		return fmt.Errorf("참여 결정은 승인·조건부·거절 중 하나: %q", p.Participation)
	}
	for _, c := range p.Conditions {
		if _, ok := ConditionCodes[c]; !ok {
			return fmt.Errorf("모르는 조건 코드: %q", c)
		}
	}
	if p.PriceFloorPct != nil && (*p.PriceFloorPct > 0 || *p.PriceFloorPct < -50) {
		return fmt.Errorf("단가 인하 하한은 -50~0(%%): %v", *p.PriceFloorPct)
	}
	if p.DecidedAt != "" {
		if _, ok := parseDate(p.DecidedAt); !ok {
			return fmt.Errorf("결정일 형식은 YYYY-MM-DD: %q", p.DecidedAt)
		}
	}
	return nil
}

// BuyerReply는 바이어 회신을 구조화한 필드다(A1 출력 또는 폼 입력). 없는 값은 null.
type BuyerReply struct {
	DeliveryStart      *string  `json:"delivery_start"`
	SplitAllowed       *bool    `json:"split_allowed"`
	PriceCutPct        *float64 `json:"price_cut_pct"`
	PenaltyBasis       *string  `json:"penalty_basis"`
	F2Acceptance       *bool    `json:"f2_acceptance"`
	InitialQtyRequired *int64   `json:"initial_qty_required"`
	ContractMonths     *int     `json:"contract_months"`
	Final              string   `json:"final,omitempty"`
}

func (p BuyerReply) validate() error {
	if p.DeliveryStart != nil {
		if _, err := time.Parse("2006-01", *p.DeliveryStart); err != nil {
			return fmt.Errorf("납품 개시월 형식은 YYYY-MM: %q", *p.DeliveryStart)
		}
	}
	if p.PriceCutPct != nil && (*p.PriceCutPct > 0 || *p.PriceCutPct < -50) {
		return fmt.Errorf("단가 인하율은 -50~0(%%): %v", *p.PriceCutPct)
	}
	if p.PenaltyBasis != nil && *p.PenaltyBasis != "backlog" && *p.PenaltyBasis != "contract" {
		return fmt.Errorf("페널티 기준은 backlog·contract: %q", *p.PenaltyBasis)
	}
	if p.InitialQtyRequired != nil && (*p.InitialQtyRequired < 0 || *p.InitialQtyRequired > 1_500_000) {
		return fmt.Errorf("첫 달 요구 수량 범위 오류: %d", *p.InitialQtyRequired)
	}
	if p.ContractMonths != nil && (*p.ContractMonths < 1 || *p.ContractMonths > 36) {
		return fmt.Errorf("계약 기간은 1~36개월: %d", *p.ContractMonths)
	}
	if p.Final != "" && p.Final != "수주" && p.Final != "실주" {
		return fmt.Errorf("종결 값은 수주·실주")
	}
	return nil
}

func (s *State) applyBuyer(p BuyerReply, note func(string, ...any)) {
	sc := &s.Scenario
	answered := map[string]bool{}
	if p.DeliveryStart != nil {
		sc.DeliveryStart, sc.StartConfirmed = *p.DeliveryStart, true
		answered["CR-04"] = true
		note("납품 개시월 %s(바이어 확인)", *p.DeliveryStart)
	}
	if p.SplitAllowed != nil {
		sc.SplitAllowed = p.SplitAllowed
		answered["CR-04"] = true
		note("분할 납품 허용 = %v", *p.SplitAllowed)
	}
	if p.ContractMonths != nil {
		sc.ContractMonths = *p.ContractMonths
		answered["CR-04"] = true
		note("계약 기간 %d개월", *p.ContractMonths)
	}
	if p.PriceCutPct != nil {
		sc.PriceCut = *p.PriceCutPct / 100
		note("단가 인하 요구 %.1f%%", *p.PriceCutPct)
	}
	if p.PenaltyBasis != nil {
		sc.PenaltyBasis, sc.PenaltyConfirmed = *p.PenaltyBasis, true
		answered["CR-05"] = true
		note("페널티 기준 = %s", map[string]string{"backlog": "지연분", "contract": "계약 총액"}[*p.PenaltyBasis])
	}
	if p.F2Acceptance != nil {
		sc.F2Accepted = p.F2Acceptance
		answered["CR-06"] = true
		note("F2 생산지 수용 = %v", *p.F2Acceptance)
	}
	if p.InitialQtyRequired != nil {
		sc.InitialQtyRequired = *p.InitialQtyRequired
		note("첫 달 필수 수량 %s EA", Commas(*p.InitialQtyRequired))
	}
	for id := range answered {
		if r := s.request(id); r != nil && r.State != "반영" {
			r.State, r.Mock = "반영", true
			r.Answer = map[string]any{"via": "E4 바이어 회신"}
		}
	}
}

// buildLines는 평가에서 제안 품목을 만든다(R6·R7·R3 조건 포함).
func buildLines(ev *Evaluation, sc Scenario, bid *Bid) []*BidLine {
	opt := map[string]OptionEval{}
	for _, o := range ev.Options {
		opt[o.OptionID] = o
	}
	var out []*BidLine
	for _, it := range sc.Items {
		pe := ev.Products[it.ProductID]
		l := &BidLine{ProductID: it.ProductID, RequestedQty: it.AnnualQty, UnitPriceUSD: it.UnitPriceUSD, PriceCut: sc.PriceCut, QtyStatus: "잠정"}
		uses := map[string]bool{}
		for _, m := range pe.Months {
			mq := MonthQty{Month: m.Month, Qty: m.Schedule}
			for _, sp := range m.Supplies {
				mq.Basis = append(mq.Basis, sp.Basis)
				uses[sp.Basis] = true
				if sp.Conditional {
					mq.Conditional = true
				}
			}
			l.Schedule = append(l.Schedule, mq)
			l.ProposedTotal += m.Schedule
		}
		if it.ProductID == ProductBasic && !sc.PL01Confirmed {
			l.QtyStatus = "미확정"
			l.Conditions = append(l.Conditions, Condition{"COND_PL01_UNCONFIRMED", fmt.Sprintf("PL-01 수량은 생산관리 확인(CR-02) 전 미확정. 확인 전 상한 월 %s EA", Commas(firstSupply(pe)))})
			l.Note = "6,000,000 EA 확정 문구 금지(R6)"
		}
		if it.ProductID == ProductDerma {
			if o := opt["OPT-02"]; o.Usable && uses["OPT-02"] {
				l.Conditions = append(l.Conditions, Condition{"COND_NO_PRE_QUAL", fmt.Sprintf("OPT-02 물량은 적격일(%s) 다음 달 %s부터", o.QualifiedDate, o.StartMonth)})
				if containsStr(o.Pending, "CR-06") {
					l.Conditions = append(l.Conditions, Condition{"COND_OPT02_F2", "OPT-02(F2 해외공장) 물량은 바이어의 생산지 수용 회신 전 조건부"})
				}
				if containsStr(o.Pending, "CR-07") {
					l.Conditions = append(l.Conditions, Condition{"COND_OPT02_MB", "OPT-02 물량은 M-B 재계약·처방 재검증 완료 전 조건부"})
				}
			}
			if o := opt["OPT-01"]; o.Usable && uses["OPT-01"] && o.Status != OptApproved {
				l.Conditions = append(l.Conditions, Condition{"COND_OPT01_STAGED", fmt.Sprintf("OPT-01 의존 물량(%s부터)은 OPT-01 승인 전 조건부", o.StartMonth)})
			}
			if ev.InitialGap > 0 && sc.MasilReleased < ev.InitialGap {
				l.Conditions = append(l.Conditions, Condition{"COND_MASIL_PROTECTED", fmt.Sprintf("바이어 첫 달 요구 중 %s EA는 Masil 보호분이라 미확정(CR-08 회신 대기)", Commas(ev.InitialGap-sc.MasilReleased))})
			}
		}
		out = append(out, l)
	}
	return out
}

// proposalChecks는 상신 전 R6·R7·R3 검사다. 엔진이 만든 제안은 통과해야 정상이다.
func proposalChecks(ev *Evaluation, sc Scenario) []string {
	var probs []string
	for _, pe := range ev.Products {
		for _, m := range pe.Months {
			if m.Schedule > m.Supply {
				probs = append(probs, fmt.Sprintf("%s %s 제안 %d > 공급 %d(R7)", pe.ProductID, m.Month, m.Schedule, m.Supply))
			}
		}
	}
	for _, o := range ev.Options {
		for _, m := range ev.Products[ProductDerma].Months {
			for _, sp := range m.Supplies {
				if sp.Basis == o.OptionID && m.Month < o.StartMonth {
					probs = append(probs, fmt.Sprintf("%s 적격 전 산입 %s(R3)", o.OptionID, m.Month))
				}
			}
		}
	}
	return probs
}

// checklist는 제출 전 점검표(R14)와 결정 신선도(R15)를 검사한다.
func (s *State) checklist(ev *Evaluation, lines []*BidLine) []string {
	var fails []string
	if len(s.Decisions) == 0 {
		return []string{"승인 결정이 없다"}
	}
	d := s.Decisions[len(s.Decisions)-1]
	if d.Freshness != "최신" {
		fails = append(fails, fmt.Sprintf("결정 %s가 낡음(R15): 재상신 필요", d.ID))
	}
	byP := map[string]*BidLine{}
	for _, l := range lines {
		byP[l.ProductID] = l
	}
	for _, c := range d.Conditions {
		switch c {
		case "COND_PL01_UNCONFIRMED":
			if l := byP[ProductBasic]; l != nil && l.QtyStatus != "미확정" && !s.Scenario.PL01Confirmed {
				fails = append(fails, "PL-01 수량이 확인 전인데 미확정이 아님")
			}
		case "COND_NO_PRE_QUAL", "COND_OPT02_F2", "COND_OPT01_STAGED":
			if probs := proposalChecks(ev, s.Scenario); len(probs) > 0 {
				fails = append(fails, probs...)
			}
		case "COND_MASIL_PROTECTED":
			// 엔진은 Masil 보호분을 공급에 넣지 않는다. 첫 달 부족분은 품목 조건으로 미확정 표시된다.
		}
	}
	if d.PriceFloor != nil && s.Scenario.PriceCut < *d.PriceFloor-1e-9 {
		fails = append(fails, fmt.Sprintf("단가 인하 %.1f%%가 승인 하한 %.1f%%보다 큼(COND_PRICE_FLOOR)", s.Scenario.PriceCut*100, *d.PriceFloor*100))
	}
	return fails
}

func contentHash(lines []*BidLine, sc Scenario) string {
	type mq struct {
		M string
		Q int64
		C bool
	}
	type li struct {
		P      string
		Status string
		S      []mq
		Conds  []string
	}
	var ls []li
	for _, l := range lines {
		x := li{P: l.ProductID, Status: l.QtyStatus}
		if x.Status == "제안됨" {
			x.Status = "잠정"
		}
		for _, m := range l.Schedule {
			x.S = append(x.S, mq{m.Month, m.Qty, m.Conditional})
		}
		for _, c := range l.Conditions {
			x.Conds = append(x.Conds, c.Code)
		}
		sort.Strings(x.Conds)
		ls = append(ls, x)
	}
	b, _ := json.Marshal(struct {
		Cut   float64
		Lines []li
	}{sc.PriceCut, ls})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

func fundingTask(opts map[string]string) string {
	parts := []string{}
	if opts["OPT-02"] == OptApproved {
		parts = append(parts, "OPT-02 12억 즉시")
	}
	switch opts["OPT-01"] {
	case OptApproved:
		parts = append(parts, "OPT-01 180억 승인")
	case OptDeferred:
		parts = append(parts, "OPT-01 180억은 선행조건 충족 시(단계 집행)")
	case OptRejected:
		parts = append(parts, "OPT-01 거절")
	}
	if len(parts) == 0 {
		parts = append(parts, "투자 없음")
	}
	return "300억 배분 조건 반영 요청: " + strings.Join(parts, ", ") + " (D5: 조건 입력만)"
}

func (s *State) request(id string) *ConfirmRequest {
	for _, r := range s.Requests {
		if r.ID == id {
			return r
		}
	}
	return nil
}

// addTask는 작업을 만든다. 같은 담당·제목의 작업이 이미 열려 있으면 새로 만들지 않고 원인·기한만 갱신한다.
func (s *State) addTask(owner, title, due string, seq int) {
	for _, t := range s.Tasks {
		if t.State == "열림" && t.Owner == owner && t.Title == title {
			t.Due, t.CauseSeq = due, seq
			return
		}
	}
	s.Tasks = append(s.Tasks, &Task{ID: fmt.Sprintf("T-%02d", len(s.Tasks)+1), Owner: owner, Title: title, Due: due, State: "열림", CauseSeq: seq})
}

// closeTasks는 제목이 prefix로 시작하는 열린 작업을 완료로 바꾼다.
func (s *State) closeTasks(prefixes ...string) {
	for _, t := range s.Tasks {
		if t.State != "열림" {
			continue
		}
		for _, p := range prefixes {
			if strings.HasPrefix(t.Title, p) {
				t.State = "완료"
			}
		}
	}
}

func validateItems(items []RFPItem) error {
	if len(items) == 0 {
		return fmt.Errorf("RFP 품목이 없다")
	}
	for _, it := range items {
		if it.ProductID != ProductBasic && it.ProductID != ProductDerma {
			return fmt.Errorf("C-201 품목은 PL-01·PL-02만: %q", it.ProductID)
		}
		if it.AnnualQty <= 0 || it.AnnualQty > 50_000_000 || it.UnitPriceUSD <= 0 || it.UnitPriceUSD > 1_000 {
			return fmt.Errorf("%s 수량·단가 범위 오류", it.ProductID)
		}
	}
	return nil
}

// addBusinessDays는 월~금 영업일 기준으로 n일을 더한다(음수면 뺀다).
func addBusinessDays(date string, n int) string {
	t, ok := parseDate(date)
	if !ok {
		return date
	}
	step := 1
	if n < 0 {
		step, n = -1, -n
	}
	for n > 0 {
		t = t.AddDate(0, 0, step)
		if wd := t.Weekday(); wd != time.Saturday && wd != time.Sunday {
			n--
		}
	}
	return fmtDate(t)
}

func num(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	}
	return 0
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
