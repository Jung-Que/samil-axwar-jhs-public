package w05

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"axwar/server/store"
)

// Service는 사건 로그(저장소)와 현재 상태를 묶는다. 상태는 로그를 처음부터 다시 적용해 만든다.
type Service struct {
	mu     sync.Mutex
	base   *Base
	st     store.Store
	ai     *AI
	events []Event
	state  *State
}

// ErrInvalid는 사건이 규칙에 맞지 않아 기록하지 않았다는 뜻이다(HTTP 409).
var ErrInvalid = errors.New("사건을 적용할 수 없다")

// NewService는 저장소의 사건을 읽어 상태를 만든다.
func NewService(ctx context.Context, base *Base, st store.Store, ai *AI) (*Service, error) {
	s := &Service{base: base, st: st, ai: ai, state: NewState(base)}
	if err := s.sync(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

// sync는 저장소 로그가 메모리와 다르면(다른 인스턴스가 쓴 경우 등) 다시 적용한다.
func (s *Service) sync(ctx context.Context) error {
	stored, err := s.st.ListEvents(ctx)
	if err != nil {
		return err
	}
	if len(stored) == len(s.events) && (len(stored) == 0 || stored[len(stored)-1].Seq == s.events[len(s.events)-1].Seq) {
		return nil
	}
	evs := make([]Event, 0, len(stored))
	for _, se := range stored {
		var e Event
		if err := json.Unmarshal(se.Body, &e); err != nil {
			return fmt.Errorf("사건 %d 해석 실패: %w", se.Seq, err)
		}
		evs = append(evs, e)
	}
	st, err := replay(s.base, evs)
	if err != nil {
		return fmt.Errorf("저장된 사건 재적용 실패: %w", err)
	}
	s.events, s.state = evs, st
	return nil
}

func replay(b *Base, evs []Event) (*State, error) {
	st := NewState(b)
	for _, e := range evs {
		if err := st.Apply(b, e); err != nil {
			return nil, fmt.Errorf("사건 %d(%s): %w", e.Seq, e.Type, err)
		}
	}
	return st, nil
}

// View는 화면이 받는 전체 상태다.
type View struct {
	State       *State            `json:"state"`
	AIMode      string            `json:"ai_mode"`
	AIModel     string            `json:"ai_model"`
	Store       string            `json:"store"`
	Files       int               `json:"raw_files_verified"`
	AsOf        string            `json:"as_of"`
	BoardDate   string            `json:"board_date"`
	Assumptions []Assumption      `json:"assumptions"`
	Scenarios   []ScenarioInfo    `json:"scenarios"`
	Conditions  map[string]string `json:"condition_codes"`
}

// Assumption은 가정 목록 한 줄이다(명세 §7). Value는 지금 쓰는 값이다.
type Assumption struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Value string `json:"value"`
	Label string `json:"label"` // 원본 | 가정 | 합성 | 계산 | 회신 반영
}

func (s *Service) view() View {
	return View{State: s.state, AIMode: s.ai.Mode(), AIModel: s.ai.Model(), Store: s.st.Kind(), Files: s.base.Files,
		AsOf: AsOf, BoardDate: BoardDate, Assumptions: assumptions(s.base, s.state.Scenario), Scenarios: Scenarios(), Conditions: ConditionCodes}
}

// Snapshot은 현재 상태를 돌려준다.
func (s *Service) Snapshot(ctx context.Context) (View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.sync(ctx); err != nil {
		return View{}, err
	}
	return s.view(), nil
}

// Submit은 사건 하나를 검증·적용하고 저장한다. 규칙에 맞지 않으면 아무것도 저장하지 않는다.
func (s *Service) Submit(ctx context.Context, typ, actor string, payload json.RawMessage, call *AiCall) (View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.sync(ctx); err != nil {
		return View{}, err
	}
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	if call != nil && len(call.Output) > 0 {
		call.HumanEdited = !sameJSON(call.Output, payload)
	}
	e := Event{Seq: len(s.events) + 1, Type: typ, Actor: actor, SimDate: s.state.Scenario.SimDate, Payload: payload, AiCall: call,
		At: time.Now().UTC().Format(time.RFC3339)}
	evs := append(append([]Event(nil), s.events...), e)
	ns, err := replay(s.base, evs)
	if err != nil {
		return s.view(), fmt.Errorf("%w: %v", ErrInvalid, unwrapLast(err))
	}
	body, err := json.Marshal(e)
	if err != nil {
		return s.view(), err
	}
	if err := s.st.AppendEvent(ctx, e.Seq, e.Type, body); err != nil {
		if errors.Is(err, store.ErrSeqTaken) {
			_ = s.sync(ctx)
			return s.view(), fmt.Errorf("%w: 다른 사용자가 먼저 사건을 넣었다 — 새로고침 후 다시", ErrInvalid)
		}
		return s.view(), err
	}
	s.events, s.state = evs, ns
	return s.view(), nil
}

// Structure는 A1 미리보기다. 상태를 바꾸지 않는다(사람이 확인한 뒤 Submit).
func (s *Service) Structure(ctx context.Context, kind, text string) *AiCall {
	return s.ai.Structure(ctx, kind, text, false)
}

// Reset은 사건 로그를 비운다(시나리오 초기화).
func (s *Service) Reset(ctx context.Context) (View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.st.ResetEvents(ctx); err != nil {
		return View{}, err
	}
	s.events, s.state = nil, NewState(s.base)
	return s.view(), nil
}

// RunScenario는 초기화 뒤 시나리오의 사건을 차례로 넣는다. 문장 단계는 녹화 응답으로 구조화한다.
func (s *Service) RunScenario(ctx context.Context, id string) (View, error) {
	sc, err := ScenarioByID(id)
	if err != nil {
		return View{}, err
	}
	if _, err := s.Reset(ctx); err != nil {
		return View{}, err
	}
	var v View
	for i, step := range sc.Steps {
		payload, call, err := s.stepPayload(ctx, step)
		if err != nil {
			return v, fmt.Errorf("시나리오 %s 단계 %d: %w", id, i+1, err)
		}
		v, err = s.Submit(ctx, step.Type, step.Actor, payload, call)
		if err != nil {
			return v, fmt.Errorf("시나리오 %s 단계 %d(%s): %w", id, i+1, step.Type, err)
		}
	}
	return v, nil
}

func (s *Service) stepPayload(ctx context.Context, step Step) (json.RawMessage, *AiCall, error) {
	if step.Text == "" {
		b, err := json.Marshal(step.Payload)
		return b, nil, err
	}
	call := s.ai.Structure(ctx, step.Kind, step.Text, true)
	if call.Validation != "pass" {
		return nil, call, fmt.Errorf("구조화 실패: %s", call.Error)
	}
	return call.Output, call, nil
}

func assumptions(b *Base, sc Scenario) []Assumption {
	lbl := func(confirmed bool) string {
		if confirmed {
			return "회신 반영"
		}
		return "가정"
	}
	masil := "미포함 → 여유에서 먼저 뺌"
	if sc.MasilInUtil {
		masil = "포함(회신) → 차감 안 함"
	}
	vw := sc.ValidationWeeks
	val := fmt.Sprintf("시나리오 %s(A 4주 / B 12주)", sc.Validation)
	if vw > 0 {
		val = fmt.Sprintf("%d주(회신)", vw)
	}
	pen := map[string]string{"backlog": "지연분 × 3%/월", "contract": "계약 총액 × 3%/월"}[sc.PenaltyBasis]
	return []Assumption{
		{"A-01", "기준일 / 이사회", AsOf + " / " + BoardDate, "원본"},
		{"A-02", "환율(as-of 종가)", fmt.Sprintf("%s %.2f", b.FXDate, b.FX), "원본"},
		{"A-03", "입찰 접수일 → 마감(RFP는 '3주'만 적음)", "접수일 = 사건 날짜, +3주", "가정"},
		{"A-04", "납품 개시월", sc.DeliveryStart, lbl(sc.StartConfirmed)},
		{"A-05", "계약 기간·월 균등 납품", fmt.Sprintf("%d개월, 연 수량 ÷ 12", sc.ContractMonths), "가정"},
		{"A-06", "스냅샷 이전 B2B 진행 주문은 가동률에 포함", "포함(다시 빼지 않음)", "가정"},
		{"A-07", "Masil SPO와 가동률", masil, lbl(sc.MasilInUtil)},
		{"A-08", "설치 후 적격까지", val, lbl(vw > 0)},
		{"A-09", "OPT 승인일(결정 전)", BoardDate + " 가정, 적격 다음 달부터 산입", "가정"},
		{"A-10", "OPT-01 → L-02, OPT-02 → F2 신규 더마 라인", "target_line 자유 텍스트 매핑", "합성"},
		{"A-11", "원료비/EA(최근월 applied × T12 원단위)", fmt.Sprintf("PL-01 %.0f원 · PL-02 %.0f원", b.MatCost[ProductBasic].PerEA, b.MatCost[ProductDerma].PerEA), "계산"},
		{"A-12", "환율 변동 시 USD 원료비 1:1 연동", "적용", "가정"},
		{"A-13", "지체상금 기준·상한", pen + ", 상한 없음", lbl(sc.PenaltyConfirmed)},
		{"A-14", "추정 납기(검산용)", "주문일 + 조건부 리드타임 중앙값", "계산"},
		{"A-15", "확인 요청 기한", "부서 5~10영업일, 바이어 마감 −5영업일", "가정"},
		{"D6", "capacity_qty 기간 단위", map[string]string{"annual": "연간 EA", "monthly": "월간(회신)"}[sc.CapacityPeriod], lbl(sc.UnitConfirmed)},
	}
}

func sameJSON(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return bytes.Equal(a, b)
	}
	xa, _ := json.Marshal(x)
	ya, _ := json.Marshal(y)
	return bytes.Equal(xa, ya)
}

func unwrapLast(err error) error {
	for {
		u := errors.Unwrap(err)
		if u == nil {
			return err
		}
		err = u
	}
}
