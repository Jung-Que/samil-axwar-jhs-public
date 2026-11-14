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
	EvSession    = "E00_SESSION" // 테스트 세션 기준일(첫 사건). 없으면 기준일 2026-07-01
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
// 입력은 모두 고정 필드(구조화된 값)다. 자유 문장·LLM 경로는 현재 제외한다(11/10 사용자 결정).
type Event struct {
	Seq     int             `json:"seq"`
	Type    string          `json:"type"`
	Actor   string          `json:"actor_role"`
	SimDate string          `json:"sim_date"`
	Payload json.RawMessage `json:"payload"`
	At      string          `json:"created_at,omitempty"`
	// By는 실제로 입력한 사용자 부서다(이슈 #43). 시뮬레이션 세션에서 다른 부서를 대행하면 Actor와 다르다.
	By string `json:"by,omitempty"`
}

// RFPFields는 RFP를 정리한 고정 필드다(E1 입력).
type RFPFields struct {
	Items         []RFPItem `json:"items"`
	Currency      string    `json:"currency"`
	PenaltyRate   float64   `json:"penalty_rate_monthly"`
	RecallBearer  string    `json:"recall_bearer"`
	DeadlineWeeks int       `json:"deadline_weeks"`
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
	MaxQty   int64          `json:"max_qty,omitempty"` // 회신 수량 상한(CR-08: 그 시점 Masil 보호량)
}

// ApproverRole은 결정 종류별 승인권자다(approval_authority_policy.md "입찰 참여·투자 승인 = CEO/이사회", R14).
// 이 역할이 아닌 입력자(영업·시스템·에이전트 등)의 결정은 기록하지 않는다(자동 확정 금지).
var ApproverRole = map[string]string{EvDecision: "CEO/이사회"}

// masilProtected는 지금 업무일까지 들어온 Masil 발주 중 제품군 p의 보호량이다(R1과 같은 집계).
// 세션을 시작한 직후에는 선택한 기준일의 보호량이다.
func masilProtected(b *Base, sc Scenario, p string) int64 {
	var q int64
	for _, sp := range b.At(sc.SimDate).SPOs {
		if sp.ProductID == p {
			q += sp.Qty
		}
	}
	return q
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
	EnteredBy     string            `json:"entered_by,omitempty"` // 시뮬레이션 대행이면 실제 입력 부서(이슈 #43)
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
	// SessionKind는 이 상태를 만든 세션 종류다(main = 기준 세션, sim = 시뮬레이션 세션, 이슈 #37).
	// 시뮬레이션 세션의 제출(E7)은 허용하되 [모의]로 표시하고, 모의 가정 사건(E9)은 시뮬레이션 세션에서만 받는다.
	SessionKind  string `json:"session_kind"`
	submittedSig string
}

// IsSim은 시뮬레이션 세션인지 본다.
func (s *State) IsSim() bool { return s.SessionKind == "sim" }

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
	// 부서별 권한표(이슈 #43): 그 사건을 그 부서가 넣을 수 있는가. 재적용에도 같은 규칙을 쓴다
	if err := eventActorAllowed(e.Type, e.Actor); err != nil {
		return err
	}
	if e.By != "" && e.By != e.Actor {
		note("[모의 대행] %s 사용자가 %s 권한으로 입력했습니다(시뮬레이션 세션).", e.By, e.Actor)
	}

	switch e.Type {
	case EvSession:
		var p struct {
			AsOf string `json:"as_of"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		if len(s.Log) > 0 || s.Bid != nil {
			return fmt.Errorf("세션 기준일은 첫 사건으로만 정할 수 있습니다. 세션을 초기화한 뒤 다시 입력해 주세요.")
		}
		if _, ok := parseDate(p.AsOf); !ok || p.AsOf < AsOfMin || p.AsOf > AsOfMax {
			return fmt.Errorf("기준일은 %s~%s 사이의 날짜(YYYY-MM-DD)로 입력해 주세요(입력 %q).", AsOfMin, AsOfMax, p.AsOf)
		}
		s.Scenario = SessionScenario(p.AsOf)
		e.SimDate = p.AsOf
		note("테스트 세션 시작: 기준일 %s(자료 %s까지), 이사회 %s, 납품 개시 가정 %s", p.AsOf, CutoffOf(p.AsOf), s.Scenario.BoardDate, s.Scenario.DeliveryStart)

	case EvClock:
		var p struct {
			SimDate string `json:"sim_date"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		if _, ok := parseDate(p.SimDate); !ok || p.SimDate < s.Scenario.SimDate {
			return fmt.Errorf("시계는 앞으로만 옮길 수 있습니다(입력 %q, 현재 %s).", p.SimDate, s.Scenario.SimDate)
		}
		prev := s.Scenario.SimDate
		s.Scenario.SimDate = p.SimDate
		e.SimDate = p.SimDate
		note("업무 날짜 → %s", p.SimDate)
		// 원본 기록으로 굴러가는 시계(이슈 #41): 그 사이 기록을 남기고 기준일 뒤 신규 수주를 여유에서 뺀다
		for _, n := range b.clockNotes(prev, p.SimDate) {
			note("%s", n)
		}
		hadHist := len(s.Scenario.HistOrders) > 0
		s.Scenario.HistOrders = b.histOrders(s.Scenario.AsOf, p.SimDate, s.Scenario.HistBasis)
		if h := s.Scenario.HistOrders; len(h) > 0 {
			note("기준일 뒤 신규 수주 반영(%s): PL-01 %s · PL-02 %s EA를 C-201 여유에서 뺌", histBasisLabel(s.Scenario.HistBasis), Commas(h[ProductBasic]), Commas(h[ProductDerma]))
		} else if hadHist {
			note("기준일보다 진행 주문이 줄어 기준일 뒤 신규 수주 차감 0(기준일 여유보다 늘려 주지는 않음)")
		}
		// 시계로 이탈 경보가 켜지면(입찰 접수 뒤) 영업 임원 확인 요청을 만든다(R13)
		if sg := atRisk(b.At(p.SimDate).Churn); sg != nil && s.Bid != nil && s.request("CR-09") == nil {
			s.Requests = append(s.Requests, &ConfirmRequest{ID: "CR-09", ToRole: "영업 임원", Question: cr09Question(*sg),
				Blocks: []string{"R13", "R12"}, Due: addBusinessDays(p.SimDate, 5), State: "요청", OpenedAt: e.Seq})
			note("이탈 경보 %s → CR-09(영업 임원) 생성", sg.CustomerID)
		}
		for _, r := range s.Requests {
			if r.State == "요청" && r.Due < p.SimDate {
				r.State = "기한 초과"
				note("%s 기한 초과(%s) → 재요청 작업", r.ID, r.Due)
				s.addTask("영업", fmt.Sprintf("재요청: %s(%s) %s", r.ID, r.ToRole, r.Question), addBusinessDays(p.SimDate, 2), e.Seq)
			}
		}

	case EvRFP:
		if s.Bid != nil {
			return fmt.Errorf("이미 접수된 입찰이 있습니다. 세션을 초기화한 뒤 다시 입력해 주세요.")
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
			return fmt.Errorf("상신은 검토중·재검토 상태에서만 할 수 있습니다(현재 %s).", s.bidState())
		}
		ev := Evaluate(b, s.Scenario)
		if probs := proposalChecks(ev, s.Scenario); len(probs) > 0 {
			return fmt.Errorf("상신 전 검사를 통과하지 못했습니다: %s", strings.Join(probs, "; "))
		}
		if s.Bid.State == BidRereview {
			s.Bid.Version++
		}
		s.Bid.State = BidSubmitted
		s.Bid.ApprovalRoute = []string{"CEO/이사회"}
		note("상신(제안 v%d) → 승인 라우팅: 입찰 참여·투자 = CEO/이사회(R14)", s.Bid.Version)
		s.closeTasks("상신안 검토", "재상신")
		s.addTask("CEO/이사회", fmt.Sprintf("승인 검토: C-201 참여·OPT-01/02 투자(제안 v%d)", s.Bid.Version), maxDate(s.Scenario.BoardDate, e.SimDate), e.Seq)

	case EvDecision:
		if s.Bid == nil || s.Bid.State != BidSubmitted {
			return fmt.Errorf("결정은 상신 상태에서만 입력할 수 있습니다(현재 %s).", s.bidState())
		}
		if want := ApproverRole[EvDecision]; e.Actor != want {
			return fmt.Errorf("승인 권한이 없습니다. 입찰 참여·투자 승인은 %s만 입력할 수 있습니다(입력자 %q, approval_authority_policy).", want, e.Actor)
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
			if p.DecidedAt < s.Scenario.BoardDate { // 승인권자(CEO/이사회) 결정은 이사회 날부터(A-09)
				p.DecidedAt = s.Scenario.BoardDate
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
				return fmt.Errorf("%s 결정 값이 올바르지 않습니다(입력 %q).", id, st)
			}
		}
		for _, d := range s.Decisions {
			d.Freshness = "낡음"
		}
		d := &Decision{ID: fmt.Sprintf("D-%02d", len(s.Decisions)+1), Approver: e.Actor, EnteredBy: e.By, Participation: p.Participation,
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
			return fmt.Errorf("열린 확인 요청이 아닙니다(%s).", p.RequestID)
		}
		// 회신은 요청 대상 부서만(부서별 권한, 이슈 #43). 바이어 요청은 E4(영업이 바이어 회신 입력)로 반영한다
		if r.ToRole != "바이어" && e.Actor != r.ToRole {
			return fmt.Errorf("회신 권한이 없습니다. %s는 %s만 회신할 수 있습니다(입력자 %q).", r.ID, r.ToRole, e.Actor)
		}
		if err := s.applyAnswer(b, r.ID, p.Answer); err != nil {
			return err
		}
		r.State, r.Answer, r.Mock = "반영", p.Answer, true
		note("%s 회신 반영(%s)", r.ID, r.ToRole)
		s.closeTasks("재요청: " + r.ID)
		if t := followUpTask(r.ID, p.Answer); t != "" { // 계산에 아직 쓰지 않는 회신은 후속 작업으로 잇는다(CR-10·CR-11)
			s.addTask(r.ToRole, t, addBusinessDays(e.SimDate, 5), e.Seq)
			note("후속 작업: %s", t)
		}

	case EvBuyer:
		if s.Bid == nil {
			return fmt.Errorf("접수된 입찰이 없습니다. RFP 접수(E1)를 먼저 입력해 주세요.")
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
			return fmt.Errorf("신규 수주는 PL-01·PL-02 제품에 수량을 0보다 크게 입력해 주세요.")
		}
		if s.Scenario.NewOrders == nil {
			s.Scenario.NewOrders = map[string]int64{}
		}
		s.Scenario.NewOrders[p.ProductID] += p.Qty
		s.NewOrders = append(s.NewOrders, map[string]any{"product_id": p.ProductID, "qty": p.Qty, "customer_id": p.CustomerID, "seq": e.Seq})
		note("스냅샷 이후 신규 수주 %s %s EA → 기본 여유에서 차감(R1)", p.ProductID, Commas(p.Qty))

	case EvSubmit:
		if s.Bid == nil || (s.Bid.State != BidApproved && s.Bid.State != BidConditional) {
			return fmt.Errorf("제출은 승인·조건부 승인 상태에서만 할 수 있습니다(현재 %s).", s.bidState())
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
		resState := "가예약"
		if s.IsSim() {
			resState = "가예약(모의)"
		}
		for _, l := range lines {
			for _, m := range l.Schedule {
				if m.Qty > 0 && l.QtyStatus != "미확정" {
					s.Reservations = append(s.Reservations, Reservation{ProductID: l.ProductID, Month: m.Month, Qty: m.Qty, Basis: strings.Join(m.Basis, "+"), State: resState})
				}
			}
		}
		s.submittedSig = contentHash(lines, s.Scenario)
		if s.IsSim() {
			note("[모의] 시뮬레이션 세션 제출입니다. 실제 제출은 아닙니다.")
		}
		note("제안 제출(점검표 통과) → 회신 대기, C-201 가예약 %d건(확정 약속을 밀지 않음, R2)", len(s.Reservations))
		s.closeTasks("제안서 작성")
		s.addTask("영업", "바이어 회신 추적(초도일·분할·페널티 기준·생산지)", addBusinessDays(e.SimDate, 5), e.Seq)

	case EvSimAssumption: // 모의 가정(이슈 #39): 시뮬레이션 세션에서만
		if !s.IsSim() {
			return fmt.Errorf("기준 세션에서는 모의 가정을 입력할 수 없습니다. '지금 상태에서 분기'로 시뮬레이션 세션을 생성한 뒤 입력해 주세요(원본 제약은 대안 시나리오로 분리, A05 §6).")
		}
		var p SimAssumption
		if err := decodeStrict(e.Payload, &p); err != nil {
			return fmt.Errorf("모의 가정 형식이 올바르지 않습니다: %v", err)
		}
		notes, err := p.apply(&s.Scenario)
		if err != nil {
			return err
		}
		if len(notes) == 0 {
			return fmt.Errorf("변경할 모의 가정이 없습니다. 바꿀 값을 하나 이상 입력해 주세요.")
		}
		s.Scenario.SimNotes = append(s.Scenario.SimNotes, notes...)
		for _, n := range notes {
			note("%s", n)
		}
		s.Scenario.HistOrders = b.histOrders(s.Scenario.AsOf, s.Scenario.SimDate, s.Scenario.HistBasis) // 반영 기준이 바뀌었을 수 있다

	default:
		return fmt.Errorf("알 수 없는 사건 종류입니다: %s", e.Type)
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
		prot := masilProtected(b, s.Scenario, ProductDerma)
		s.Requests = append(s.Requests, &ConfirmRequest{ID: "CR-08", ToRole: "Masil SCM",
			Question: fmt.Sprintf("Masil 더마 발주(업무일 %s까지 %s EA) 중 최대 %s EA의 입고를 늦출 수 있습니까(C-201 초도 %s)?", CutoffOf(s.Scenario.SimDate), Commas(prot), Commas(min64(s.Eval.InitialGap, prot)), s.Scenario.DeliveryStart),
			Blocks:   []string{"R2", "PL-02 첫 달 수량"}, Due: addBusinessDays(s.Scenario.SimDate, 3), State: "요청", OpenedAt: seq, MaxQty: prot})
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
			add("CR-03", "생산관리", fmt.Sprintf("Masil SPO(%s %s EA)가 %s 계획(가동률 %.0f%%)에 들어 있습니까?", l.ProductID, Commas(l.MasilProtected), l.LineID, l.UtilPct), addBusinessDays(recv, 5), "R1", "A-07")
			break
		}
	}
	add("CR-04", "바이어", "납품 개시일·분할 납품 허용·계약 기간", addBusinessDays(dl, -5), "R7", "R8", "A-04", "A-05")
	add("CR-05", "바이어", "납기 지연 페널티의 기준액(지연분·계약 총액)과 상한", addBusinessDays(dl, -5), "R8", "A-13")
	add("CR-06", "바이어", "F2 해외공장 생산지(OPT-02) 수용 여부", addBusinessDays(dl, -5), "R3", "OPT-02")
	add("CR-07", "구매·품질", "M-B 재계약 가능 여부와 처방 재검증 기간(주)", addBusinessDays(recv, 10), "R3", "OPT-02", "A-08")
	// 원본 정보가 없어 계산으로 풀 수 없는 개선은 확인 요청으로 남긴다(이슈 #35). 회신은 기록·후속 작업만 하고,
	// 계산 반영(타 라인 공급, 추정 납기 대체)은 설계 단계다.
	var others []string
	var otherSpare int64
	for _, l := range otherLinesForBasic(ev) {
		others = append(others, l.LineID+" "+Commas(l.SpareAnnual))
		otherSpare += l.SpareAnnual
	}
	add("CR-10", "생산관리", fmt.Sprintf("PL-01을 다른 라인에서 생산할 수 있습니까? 라인별 전환·셋업 기간(주)과 연간 가능 수량을 알려 주세요(능력 스냅샷 여유 %s, 합 %s EA). 원본은 PL-01을 F1 전용으로 두지 않습니다.",
		strings.Join(others, " · "), Commas(otherSpare)), addBusinessDays(recv, 5), "A-16", "R6")
	cm := ev.Commitments
	add("CR-11", "영업", fmt.Sprintf("진행 주문 %s건(PL-01 %s건 · PL-02 %s건)의 고객 합의 납기·수량 대응표. 현재는 리드타임 통계로 추정합니다(A-14).",
		Commas(int64(cm.InProgressCount)), Commas(int64(cm.CountByProduct[ProductBasic])), Commas(int64(cm.CountByProduct[ProductDerma]))), addBusinessDays(recv, 5), "A-14", "R8")
	// R13: 이탈 경보(AT_RISK) 고객이 있으면 그 능력을 C-201에 돌려도 되는지 영업 임원에게 묻는다(자동 반환 금지)
	if sg := atRisk(ev.Churn); sg != nil {
		add("CR-09", "영업 임원", cr09Question(*sg), addBusinessDays(recv, 5), "R13", "R12")
	}
}

// otherLinesForBasic은 PL-01을 옮겨 만들 후보 라인이다: PL-01 자기 라인과 더마 전용 라인(L-02)을 뺀 기존 라인.
func otherLinesForBasic(ev *Evaluation) []LineEval {
	var out []LineEval
	for _, l := range ev.Lines {
		if l.ProductID != ProductBasic && l.ProductID != ProductDerma && !l.Dedicated {
			out = append(out, l)
		}
	}
	return out
}

// followUpTask는 계산에 아직 쓰지 않는 회신(CR-10·CR-11)의 후속 작업 제목이다. 회신 검증은 applyAnswer가 먼저 했다.
func followUpTask(id string, a map[string]any) string {
	switch id {
	case "CR-10":
		lines := anyList(a["lines"])
		if len(lines) == 0 {
			return "PL-01 타 라인 배정 불가 회신: PL-01 수량은 L-01 여유 기준 유지, 증산·외주 대안 검토(CR-02)"
		}
		return fmt.Sprintf("PL-01 타 라인 배정안 검토: %s, 전환·셋업 %d주, 연 %s EA(엔진 공급 반영은 설계 단계)",
			strings.Join(lines, "·"), int(num(a["changeover_weeks"])), Commas(int64(num(a["annual_qty"]))))
	case "CR-11":
		if v, _ := a["received"].(bool); v {
			return fmt.Sprintf("확정 납기 대응표(%s건)로 추정 납기(A-14) 대체 검토(엔진 반영은 설계 단계)", Commas(int64(num(a["orders"]))))
		}
		return "확정 납기 대응표 없음: 추정 납기(A-14) 유지, 주요 고객별 납기 재확인 요청"
	}
	return ""
}

func anyList(v any) []string {
	xs, _ := v.([]any)
	var out []string
	for _, x := range xs {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// 확인 요청 회신은 요청 대상 부서(ToRole)만 넣는다(이슈 #43). CR-09(기존 고객 약속 조정)는 영업 임원이다
// (approval_authority_policy "C-101 재협상/판가 = 영업 임원", 가정 A-20).

// applyAnswer는 내부 부서 회신(E3)을 가정·시나리오에 반영한다.
func (s *State) applyAnswer(b *Base, id string, a map[string]any) error {
	switch id {
	case "CR-01":
		v, _ := a["capacity_period"].(string)
		if v != "annual" && v != "monthly" {
			return fmt.Errorf("CR-01 회신: 기간 단위는 연간·월간 중에서 선택해 주세요.")
		}
		s.Scenario.CapacityPeriod, s.Scenario.UnitConfirmed = v, true
	case "CR-02":
		s.Scenario.PL01Confirmed = true
		s.Scenario.PL01ExtraAnnual = int64(num(a["extra_annual"]))
	case "CR-03":
		v, ok := a["masil_in_util"].(bool)
		if !ok {
			return fmt.Errorf("CR-03 회신: Masil 발주가 계획에 포함되는지(예·아니오) 선택해 주세요.")
		}
		s.Scenario.MasilInUtil = v
	case "CR-07":
		v, ok := a["mb_ready"].(bool)
		if !ok {
			return fmt.Errorf("CR-07 회신: M-B 재계약 가능 여부(가능·불가)를 선택해 주세요.")
		}
		s.Scenario.MBReady = v
		if w := int(num(a["validation_weeks"])); w > 0 {
			s.Scenario.ValidationWeeks = w
		}
	case "CR-08":
		// 상한 = 지금 업무일(세션 시작 직후면 선택한 기준일)의 PL-02 Masil 보호량. 보호분보다 많이 풀 수는 없다
		limit := masilProtected(b, s.Scenario, ProductDerma)
		q := int64(num(a["released_qty"]))
		if q < 0 || q > limit {
			return fmt.Errorf("CR-08 회신: 늦출 수 있는 수량은 0~%s EA 사이로 입력해 주세요(업무일 %s까지 PL-02 Masil 보호량).", Commas(limit), CutoffOf(s.Scenario.SimDate))
		}
		s.Scenario.MasilReleased = q
	case "CR-10": // 기록만(엔진 반영은 설계 단계). 라인 코드·주수·수량만 검증한다
		ev := Evaluate(b, s.Scenario)
		spare := map[string]int64{}
		for _, l := range otherLinesForBasic(ev) {
			spare[l.LineID] = l.SpareAnnual
		}
		var limit int64
		for _, id := range anyList(a["lines"]) {
			q, ok := spare[id]
			if !ok {
				return fmt.Errorf("CR-10 회신: %s는 PL-01 후보 라인이 아닙니다. L-01과 더마 전용 L-02를 제외한 기존 라인만 선택해 주세요.", id)
			}
			limit += q
		}
		if w := num(a["changeover_weeks"]); w < 0 || w > 104 {
			return fmt.Errorf("CR-10 회신: 전환·셋업 기간은 0~104주 사이로 입력해 주세요.")
		}
		if q := int64(num(a["annual_qty"])); q < 0 || q > limit {
			return fmt.Errorf("CR-10 회신: 연간 가능 수량은 0~%s EA 사이로 입력해 주세요(선택한 라인의 능력 스냅샷 여유 합).", Commas(limit))
		}
	case "CR-11": // 기록만(엔진 반영은 설계 단계)
		if _, ok := a["received"].(bool); !ok {
			return fmt.Errorf("CR-11 회신: 확정 납기 대응표를 받았는지(받음·없음) 선택해 주세요.")
		}
		n := int64(num(a["orders"]))
		total := int64(b.At(s.Scenario.SimDate).Commit.InProgressCount)
		if n < 0 || n > total {
			return fmt.Errorf("CR-11 회신: 주문 수는 0~%s건 사이로 입력해 주세요(진행 주문 수).", Commas(total))
		}
	case "CR-09":
		revised, ok := a["revised"].(bool)
		if !ok {
			return fmt.Errorf("CR-09 회신: 계약·예측 수정 확인 여부(예·아니오)를 선택해 주세요.")
		}
		sg := atRisk(b.At(s.Scenario.SimDate).Churn)
		if sg == nil {
			return fmt.Errorf("CR-09 회신: 현재 업무일에는 이탈 경보 고객이 없습니다.")
		}
		rel := map[string]int64{ProductBasic: int64(num(a["release_pl01"])), ProductDerma: int64(num(a["release_pl02"]))}
		for p, q := range rel {
			if q < 0 || q > sg.T12BookedQty[p] {
				return fmt.Errorf("CR-09 회신: %s 반환 수량은 0~%s EA 사이로 입력해 주세요(최근 12개월 %s 수주).", p, Commas(sg.T12BookedQty[p]), sg.CustomerID)
			}
			if !revised && q > 0 {
				return fmt.Errorf("CR-09 회신: 계약·예측 수정이 확인되지 않으면 반환 수량은 0으로 입력해 주세요.")
			}
		}
		s.Scenario.ChurnAnswered, s.Scenario.ChurnRevised = true, revised
		if revised {
			s.Scenario.ChurnRelease = rel
		} else {
			s.Scenario.ChurnRelease = nil
		}
	default:
		return fmt.Errorf("%s는 바이어 회신(E4)으로 입력해 주세요.", id)
	}
	return nil
}

// DecisionFields는 승인권자 회신을 정리한 고정 필드다.
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
		return fmt.Errorf("참여 결정은 승인·조건부·거절 중에서 선택해 주세요(입력 %q).", p.Participation)
	}
	for _, c := range p.Conditions {
		if _, ok := ConditionCodes[c]; !ok {
			return fmt.Errorf("알 수 없는 조건 코드입니다: %q", c)
		}
	}
	if p.PriceFloorPct != nil && (*p.PriceFloorPct > 0 || *p.PriceFloorPct < -50) {
		return fmt.Errorf("단가 인하 하한은 -50~0%% 사이로 입력해 주세요(입력 %v).", *p.PriceFloorPct)
	}
	if p.DecidedAt != "" {
		if _, ok := parseDate(p.DecidedAt); !ok {
			return fmt.Errorf("결정일은 YYYY-MM-DD 형식으로 입력해 주세요(입력 %q).", p.DecidedAt)
		}
	}
	return nil
}

// BuyerReply는 바이어 회신을 정리한 고정 필드다. 없는 값은 null.
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
			return fmt.Errorf("납품 개시월은 YYYY-MM 형식으로 입력해 주세요(입력 %q).", *p.DeliveryStart)
		}
	}
	if p.PriceCutPct != nil && (*p.PriceCutPct > 0 || *p.PriceCutPct < -50) {
		return fmt.Errorf("단가 인하율은 -50~0%% 사이로 입력해 주세요(입력 %v).", *p.PriceCutPct)
	}
	if p.PenaltyBasis != nil && *p.PenaltyBasis != "backlog" && *p.PenaltyBasis != "contract" {
		return fmt.Errorf("페널티 기준은 지연분(backlog)·계약 총액(contract) 중에서 선택해 주세요(입력 %q).", *p.PenaltyBasis)
	}
	if p.InitialQtyRequired != nil && (*p.InitialQtyRequired < 0 || *p.InitialQtyRequired > 1_500_000) {
		return fmt.Errorf("첫 달 요구 수량은 0~1,500,000 EA 사이로 입력해 주세요(입력 %d).", *p.InitialQtyRequired)
	}
	if p.ContractMonths != nil && (*p.ContractMonths < 1 || *p.ContractMonths > 36) {
		return fmt.Errorf("계약 기간은 1~36개월 사이로 입력해 주세요(입력 %d).", *p.ContractMonths)
	}
	if p.Final != "" && p.Final != "수주" && p.Final != "실주" {
		return fmt.Errorf("종결 값은 수주·실주 중에서 선택해 주세요.")
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
		return []string{"승인 결정이 없습니다."}
	}
	d := s.Decisions[len(s.Decisions)-1]
	if d.Freshness != "최신" {
		fails = append(fails, fmt.Sprintf("결정 %s가 낡았습니다(R15). 재상신이 필요합니다.", d.ID))
	}
	byP := map[string]*BidLine{}
	for _, l := range lines {
		byP[l.ProductID] = l
	}
	for _, c := range d.Conditions {
		switch c {
		case "COND_PL01_UNCONFIRMED":
			if l := byP[ProductBasic]; l != nil && l.QtyStatus != "미확정" && !s.Scenario.PL01Confirmed {
				fails = append(fails, "PL-01 수량이 생산관리 확인 전인데 미확정으로 표시되지 않았습니다.")
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
		fails = append(fails, fmt.Sprintf("단가 인하 %.1f%%가 승인 하한 %.1f%%보다 큽니다(COND_PRICE_FLOOR).", s.Scenario.PriceCut*100, *d.PriceFloor*100))
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
		return fmt.Errorf("RFP 품목이 없습니다. 품목을 하나 이상 입력해 주세요.")
	}
	for _, it := range items {
		if it.ProductID != ProductBasic && it.ProductID != ProductDerma {
			return fmt.Errorf("C-201 품목은 PL-01·PL-02만 입력할 수 있습니다(입력 %q).", it.ProductID)
		}
		if it.AnnualQty <= 0 || it.AnnualQty > 50_000_000 || it.UnitPriceUSD <= 0 || it.UnitPriceUSD > 1_000 {
			return fmt.Errorf("%s 수량·단가가 허용 범위를 벗어났습니다. 수량은 50,000,000 EA 이하, 단가는 USD 1,000 이하의 양수로 입력해 주세요.", it.ProductID)
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

func maxDate(a, b string) string {
	if a > b {
		return a
	}
	return b
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
