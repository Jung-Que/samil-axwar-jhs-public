package w05

import (
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
	events []Event
	state  *State
}

// ErrInvalid는 사건이 규칙에 맞지 않아 기록하지 않았다는 뜻이다(HTTP 409).
var ErrInvalid = errors.New("사건을 적용할 수 없다")

// NewService는 저장소의 사건을 읽어 상태를 만든다.
func NewService(ctx context.Context, base *Base, st store.Store) (*Service, error) {
	s := &Service{base: base, st: st, state: NewState(base)}
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
	Input       string            `json:"input_mode"`
	Store       string            `json:"store"`
	Files       int               `json:"raw_files_verified"`
	AsOf        string            `json:"as_of"`      // 세션 기준일
	BoardDate   string            `json:"board_date"` // 이사회 = 기준일 + 5일
	Cutoff      string            `json:"cutoff"`     // 지금 쓰는 기록의 마지막 날
	AsOfMin     string            `json:"as_of_min"`
	AsOfMax     string            `json:"as_of_max"`
	DataEnd     string            `json:"data_end"`
	Lanes       []Lane            `json:"lanes"`
	Lines       []LineRow         `json:"line_board"`
	Assumptions []Assumption      `json:"assumptions"`
	Basis       DataBasis         `json:"data_basis"` // 수치 출처·단위·기간, 원본 간 규모 차이(표시용)
	Scenarios   []ScenarioInfo    `json:"scenarios"`
	Conditions  map[string]string `json:"condition_codes"`
}

// Assumption은 가정 목록 한 줄이다(명세 §7). Value는 지금 쓰는 값, Affects는 그 가정을 쓰는 판단·화면이다.
type Assumption struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Value   string `json:"value"`
	Label   string `json:"label"` // 원본 | 가정 | 합성 | 계산 | 회신 반영 | 미래 자료
	Affects string `json:"affects"`
}

func (s *Service) view() View {
	sc := s.state.Scenario
	return View{State: s.state, Input: "고정 필드(자유 문장·LLM 현재 제외)", Store: s.st.Kind(), Files: s.base.Files,
		AsOf: sc.AsOf, BoardDate: sc.BoardDate, Cutoff: CutoffOf(sc.SimDate), AsOfMin: AsOfMin, AsOfMax: AsOfMax, DataEnd: DataEnd,
		Lanes: s.state.Lanes(s.base), Lines: s.state.LineBoard(s.base),
		Assumptions: assumptions(s.base, sc), Basis: s.state.Basis(s.base), Scenarios: Scenarios(sc.AsOf), Conditions: ConditionCodes}
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
func (s *Service) Submit(ctx context.Context, typ, actor string, payload json.RawMessage) (View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.sync(ctx); err != nil {
		return View{}, err
	}
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	e := Event{Seq: len(s.events) + 1, Type: typ, Actor: actor, SimDate: s.state.Scenario.SimDate, Payload: payload,
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

// Reset은 사건 로그를 비우고, asOf가 있으면 그 기준일로 테스트 세션을 시작한다(E00_SESSION).
func (s *Service) Reset(ctx context.Context, asOf string) (View, error) {
	if asOf != "" {
		if _, ok := parseDate(asOf); !ok || asOf < AsOfMin || asOf > AsOfMax {
			return View{}, fmt.Errorf("%w: 기준일은 %s~%s 사이(YYYY-MM-DD): %q", ErrInvalid, AsOfMin, AsOfMax, asOf)
		}
	}
	s.mu.Lock()
	if err := s.st.ResetEvents(ctx); err != nil {
		s.mu.Unlock()
		return View{}, err
	}
	s.events, s.state = nil, NewState(s.base)
	s.mu.Unlock()
	if asOf == "" || asOf == DefaultAsOf {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.view(), nil
	}
	return s.Submit(ctx, EvSession, "시스템", json.RawMessage(fmt.Sprintf(`{"as_of":%q}`, asOf)))
}

// Inbox는 접수함이다(최근 days일, 최대 limit건).
func (s *Service) Inbox(ctx context.Context, days, limit int) (Inbox, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.sync(ctx); err != nil {
		return Inbox{}, err
	}
	return s.state.Inbox(s.base, days, limit), nil
}

// Backtest는 정답 비교다.
func (s *Service) Backtest(ctx context.Context) (Backtest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.sync(ctx); err != nil {
		return Backtest{}, err
	}
	return s.state.Backtest(s.base), nil
}

// RunScenario는 기준일 asOf(비면 지금 세션 기준일)로 초기화한 뒤 시나리오의 사건을 고정 입력값으로 차례로 넣는다.
func (s *Service) RunScenario(ctx context.Context, id, asOf string) (View, error) {
	if asOf == "" {
		s.mu.Lock()
		asOf = s.state.Scenario.AsOf
		s.mu.Unlock()
	}
	sc, err := ScenarioByID(id, asOf)
	if err != nil {
		return View{}, err
	}
	if _, err := s.Reset(ctx, asOf); err != nil {
		return View{}, err
	}
	var v View
	for i, step := range sc.Steps {
		payload, err := stepJSON(step)
		if err != nil {
			return v, fmt.Errorf("시나리오 %s 단계 %d: %w", id, i+1, err)
		}
		v, err = s.Submit(ctx, step.Type, step.Actor, payload)
		if err != nil {
			return v, fmt.Errorf("시나리오 %s 단계 %d(%s): %w", id, i+1, step.Type, err)
		}
	}
	return v, nil
}

func assumptions(b *Base, sc Scenario) []Assumption {
	at := b.At(sc.SimDate)
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
	masilQty := map[string]int64{}
	for _, sp := range at.SPOs {
		masilQty[sp.ProductID] += sp.Qty
	}
	agN, agQty := b.AgencyOpen(at.Cutoff)
	agApprox := ""
	if agQty >= 10_000 {
		agApprox = fmt.Sprintf("(약 %.0f만 EA)", float64(agQty)/1e4)
	}
	f2, f2Label := "필요(원본에 없는 조건) · CR-06 회신 전", "가정"
	if sc.F2Accepted != nil {
		f2Label = "회신 반영"
		f2 = "바이어 수용(회신)"
		if !*sc.F2Accepted {
			f2 = "바이어 미수용(회신) → OPT-02를 C-201에 쓰지 않음"
		}
	}
	boardLabel := "원본"
	if sc.AsOf != DefaultAsOf {
		boardLabel = "가정" // 원본은 기준일 2026-07-01의 이사회(+5일)만 정한다. 다른 기준일은 같은 간격을 빌려 쓴다
	}
	return []Assumption{
		{"A-01", "기준일 / 이사회 / 쓰는 기록의 마지막 날", sc.AsOf + " / " + sc.BoardDate + " / " + at.Cutoff, "원본", "결정 가능일·OPT 결정일 기본값, 기준일 뒤 기록 차단"},
		{"A-02", "환율(기준일 이전 마지막 종가)", fmt.Sprintf("%s %.2f", at.FXDate, at.FX), "원본", "원화 단가·매출·공헌이익(R9)"},
		{"A-03", "입찰 접수일 → 마감(RFP는 '3주'만 적음)", "접수일 = 사건 날짜, +3주", "가정", "마감, 바이어 확인 요청 기한"},
		{"A-04", "납품 개시월", sc.DeliveryStart, lbl(sc.StartConfirmed), "월별 요구·공급·부족·지체상금(R7·R8)"},
		{"A-05", "계약 기간·월 균등 납품", fmt.Sprintf("%d개월, 연 수량 ÷ 12", sc.ContractMonths), "가정", "월 요구량(R7)"},
		{"A-06", "스냅샷 이전 B2B 진행 주문은 가동률에 포함", "포함(다시 빼지 않음)", "가정", "기본 여유(R1): 진행 주문을 여유에서 다시 빼지 않음"},
		{"A-07", "Masil SPO와 가동률", masil, lbl(sc.MasilInUtil), "C-201 기본 가용(R1), CR-03"},
		{"A-08", "설치 후 적격까지", val, lbl(vw > 0), "OPT 적격일·산입 시작월(R3)"},
		{"A-09", "OPT 승인일(결정 전)", sc.BoardDate + " 가정, 적격 다음 달부터 산입", "가정", "OPT 설치·적격일(R3)"},
		{"A-10", "OPT-01 → L-02, OPT-02 → F2 신규 더마 라인", "target_line 자유 텍스트 매핑", "합성", "라인 보드 F2 신규 라인, 가예약 라인"},
		{"A-11", "원료비/EA(최근월 applied × T12 원단위)", fmt.Sprintf("PL-01 %.0f원 · PL-02 %.0f원(단가 기준월 %s)", at.MatCost[ProductBasic].PerEA, at.MatCost[ProductDerma].PerEA, at.MatCost[ProductDerma].LastMonth), "계산", "공헌이익·손익분기 환율·회수기간(R9)"},
		{"A-12", "환율 변동 시 USD 원료비 1:1 연동", "적용", "가정", "환율 민감도의 공헌이익(R9)"},
		{"A-13", "지체상금 기준·상한", pen + ", 상한 없음", lbl(sc.PenaltyConfirmed), "지체상금 노출(R8)"},
		{"A-14", "추정 납기(검산용)", "주문일 + 조건부 리드타임 중앙값", "계산", "라인 보드 부하·상태, 약속 원장 추정 납품, 정답 비교"},
		{"A-15", "확인 요청 기한", "부서 5~10영업일, 바이어 마감 −5영업일", "가정", "확인 요청 기한·기한 초과 재요청"},
		{"T2", "능력 스냅샷", DataEnd + map[bool]string{true: " 자료를 더 이른 기준일에 씀", false: " 자료"}[at.FutureSnapshot], map[bool]string{true: "미래 자료", false: "원본"}[at.FutureSnapshot], "과거 기준일의 여유 전체(R1)"},
		{"D6", "capacity_qty 기간 단위", map[string]string{"annual": "연간 EA", "monthly": "월간(회신)"}[sc.CapacityPeriod], lbl(sc.UnitConfirmed), "기본 여유·C-201 가용(R1). 배치 실적과의 규모 차이는 '원본 간 규모 차이' 표"},
		{"A-16", "PL-01 생산 라인 = L-01 하나(라인 1개 = 제품군 1개)", "PL-01 여유는 L-01만 봄. 원본은 PL-01을 F1 전용으로 두지 않음(is_f1_dedicated=False)", "가정", "PL-01 상한(R6)·PL-01 차단 플래그"},
		{"A-17", "대리점 주문(customer_id=NONE) 제외", fmt.Sprintf("%s건 · %s EA%s가 진행 주문·부하에서 빠짐(대리점 주문은 원본 전체에 출하 기록 0건)", Commas(int64(agN)), Commas(agQty), agApprox), "가정", "진행 주문 건수·수량, 라인 보드 부하·상태(PL-01·PL-04·PL-06)"},
		{"A-18", "OPT-02에 바이어의 F2 생산지 수용 필요", f2, f2Label, "OPT-02 물량 조건부(COND_OPT02_F2), 미수용이면 OPT-02 제외"},
		{"A-19", "Masil 발주 우선 보호(우선순위는 원본에 없음. 근거: Masil 더마 Corevia 100% 의존)", fmt.Sprintf("PL-01 %s · PL-02 %s EA를 C-201보다 먼저 보호", Commas(masilQty[ProductBasic]), Commas(masilQty[ProductDerma])), "가정", "C-201 기본 가용(R1), CR-03·CR-08, COND_MASIL_PROTECTED"},
		{"A-20", "기존 고객 납기·판가 변경 승인 = 영업 임원", "원본은 'C-101 재협상/판가 = 영업 임원'만 명시. 모든 기존 고객으로 일반화, 변경 흐름은 설계 단계", "가정", "변경 요청 승인 라우팅(R2·R14)"},
		{"A-21", "라인 보드 상태 문턱", "다음 달 부하 ÷ 배치 실적 월 생산: 100% 초과면 초과, 85% 이상이면 포화(표시용, 판단에 안 씀)", "가정", "생산 라인 보드 상태(여유·포화·초과)"},
		{"A-22", "이사회 = 기준일 + 5일(모든 기준일에 적용)", fmt.Sprintf("%s(%s) · 주말 보정 없음", sc.BoardDate, weekdayKo(sc.BoardDate)), boardLabel, "결정 가능일, 승인 검토 작업 기한, OPT 결정일 기본값"},
	}
}

func weekdayKo(date string) string {
	t, ok := parseDate(date)
	if !ok {
		return "?"
	}
	return []string{"일", "월", "화", "수", "목", "금", "토"}[t.Weekday()]
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
