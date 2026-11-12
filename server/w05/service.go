package w05

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"axwar/server/store"
)

// Service는 세션별 사건 로그(저장소)와 현재 상태를 묶는다. 상태는 로그를 처음부터 다시 적용해 만든다.
// 세션(이슈 #37): main = 기준 세션(서비스 단계의 운영 세션), sim = 시뮬레이션 세션(분기·시연·모의 가정).
// 이름에 In이 없는 메서드(Submit·Snapshot·Reset·RunScenario 등)는 기준 세션(main)을 다룬다.
type Service struct {
	mu   sync.Mutex
	base *Base
	st   store.Store
	sess map[string]*session
}

type session struct {
	meta   store.Session
	events []Event
	state  *State
}

// ErrInvalid는 사건이 규칙에 맞지 않아 기록하지 않았다는 뜻이다(HTTP 409).
var ErrInvalid = errors.New("사건을 적용할 수 없다")

// ErrNotFound는 없는 세션을 가리켰다는 뜻이다(HTTP 404).
var ErrNotFound = errors.New("세션이 없다")

// NewService는 저장소의 기준 세션 사건을 읽어 상태를 만든다. 다른 세션은 처음 쓸 때 읽는다.
func NewService(ctx context.Context, base *Base, st store.Store) (*Service, error) {
	s := &Service{base: base, st: st, sess: map[string]*session{}}
	if _, err := s.load(ctx, store.MainSession); err != nil {
		return nil, err
	}
	return s, nil
}

// load는 세션을 저장소와 맞춘다. 저장소 로그가 메모리와 다르면(다른 인스턴스가 쓴 경우 등) 다시 적용한다. 잠금을 잡고 부른다.
func (s *Service) load(ctx context.Context, id string) (*session, error) {
	if id == "" {
		id = store.MainSession
	}
	cs := s.sess[id]
	if cs == nil {
		metas, err := s.st.ListSessions(ctx)
		if err != nil {
			return nil, err
		}
		for _, m := range metas {
			if m.ID == id {
				cs = &session{meta: m}
				break
			}
		}
		if cs == nil {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
		}
	}
	stored, err := s.st.ListEvents(ctx, id)
	if errors.Is(err, store.ErrNoSession) {
		delete(s.sess, id)
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	if cs.state != nil && len(stored) == len(cs.events) && (len(stored) == 0 || stored[len(stored)-1].Seq == cs.events[len(cs.events)-1].Seq) {
		return cs, nil
	}
	evs := make([]Event, 0, len(stored))
	for _, se := range stored {
		var e Event
		if err := json.Unmarshal(se.Body, &e); err != nil {
			return nil, fmt.Errorf("세션 %s 사건 %d 해석 실패: %w", id, se.Seq, err)
		}
		evs = append(evs, e)
	}
	st, err := replay(s.base, evs, cs.meta.Kind)
	if err != nil {
		return nil, fmt.Errorf("세션 %s 저장된 사건 재적용 실패: %w", id, err)
	}
	cs.events, cs.state = evs, st
	s.sess[id] = cs
	return cs, nil
}

func newState(b *Base, kind string) *State {
	st := NewState(b)
	st.SessionKind = kind
	return st
}

func replay(b *Base, evs []Event, kind string) (*State, error) {
	st := newState(b, kind)
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
	Session     SessionInfo       `json:"session"`  // 지금 보는 세션
	Sessions    []SessionInfo     `json:"sessions"` // 전체 세션 목록(세션 선택기)
}

// SessionInfo는 화면에 보이는 세션 정보다.
type SessionInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"` // main | sim
	ParentID  string `json:"parent_id,omitempty"`
	ForkSeq   int    `json:"fork_seq,omitempty"`
	CreatedAt string `json:"created_at"`
	Events    int    `json:"events"`
}

func sessionInfo(m store.Session, events int) SessionInfo {
	return SessionInfo{ID: m.ID, Name: m.Name, Kind: m.Kind, ParentID: m.ParentID, ForkSeq: m.ForkSeq,
		CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339), Events: events}
}

// Assumption은 가정 목록 한 줄이다(명세 §7). Value는 지금 쓰는 값, Affects는 그 가정을 쓰는 판단·화면이다.
type Assumption struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Value   string `json:"value"`
	Label   string `json:"label"` // 원본 | 가정 | 합성 | 계산 | 회신 반영 | 미래 자료
	Affects string `json:"affects"`
}

func (s *Service) view(ctx context.Context, cs *session) View {
	st := cs.state
	sc := st.Scenario
	v := View{State: st, Input: "고정 필드(자유 문장·LLM 현재 제외)", Store: s.st.Kind(), Files: s.base.Files,
		AsOf: sc.AsOf, BoardDate: sc.BoardDate, Cutoff: CutoffOf(sc.SimDate), AsOfMin: AsOfMin, AsOfMax: AsOfMax, DataEnd: DataEnd,
		Lanes: st.Lanes(s.base), Lines: st.LineBoard(s.base),
		Assumptions: assumptions(s.base, sc), Basis: st.Basis(s.base), Scenarios: Scenarios(sc.AsOf), Conditions: ConditionCodes,
		Session: sessionInfo(cs.meta, len(cs.events))}
	if metas, err := s.st.ListSessions(ctx); err == nil {
		for _, m := range metas {
			n := -1
			if c := s.sess[m.ID]; c != nil {
				n = len(c.events)
			}
			v.Sessions = append(v.Sessions, sessionInfo(m, n))
		}
	}
	return v
}

// Snapshot은 기준 세션의 현재 상태를 돌려준다.
func (s *Service) Snapshot(ctx context.Context) (View, error) {
	return s.SnapshotIn(ctx, store.MainSession)
}

// SnapshotIn은 세션의 현재 상태를 돌려준다.
func (s *Service) SnapshotIn(ctx context.Context, sid string) (View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cs, err := s.load(ctx, sid)
	if err != nil {
		return View{}, err
	}
	return s.view(ctx, cs), nil
}

// Submit은 기준 세션에 사건 하나를 넣는다.
func (s *Service) Submit(ctx context.Context, typ, actor string, payload json.RawMessage) (View, error) {
	return s.SubmitIn(ctx, store.MainSession, typ, actor, payload)
}

// SubmitIn은 세션에 사건 하나를 검증·적용하고 저장한다. 규칙에 맞지 않으면 아무것도 저장하지 않는다.
func (s *Service) SubmitIn(ctx context.Context, sid, typ, actor string, payload json.RawMessage) (View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.submit(ctx, sid, typ, actor, payload)
}

func (s *Service) submit(ctx context.Context, sid, typ, actor string, payload json.RawMessage) (View, error) {
	cs, err := s.load(ctx, sid)
	if err != nil {
		return View{}, err
	}
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	e := Event{Seq: len(cs.events) + 1, Type: typ, Actor: actor, SimDate: cs.state.Scenario.SimDate, Payload: payload,
		At: time.Now().UTC().Format(time.RFC3339)}
	evs := append(append([]Event(nil), cs.events...), e)
	ns, err := replay(s.base, evs, cs.meta.Kind)
	if err != nil {
		return s.view(ctx, cs), fmt.Errorf("%w: %v", ErrInvalid, unwrapLast(err))
	}
	body, err := json.Marshal(e)
	if err != nil {
		return s.view(ctx, cs), err
	}
	if err := s.st.AppendEvent(ctx, cs.meta.ID, e.Seq, e.Type, body); err != nil {
		if errors.Is(err, store.ErrSeqTaken) {
			if c2, err2 := s.load(ctx, cs.meta.ID); err2 == nil {
				cs = c2
			}
			return s.view(ctx, cs), fmt.Errorf("%w: 다른 사용자가 먼저 사건을 넣었다 — 새로고침 후 다시", ErrInvalid)
		}
		return s.view(ctx, cs), err
	}
	cs.events, cs.state = evs, ns
	return s.view(ctx, cs), nil
}

func validAsOf(asOf string) error {
	if asOf == "" {
		return nil
	}
	if _, ok := parseDate(asOf); !ok || asOf < AsOfMin || asOf > AsOfMax {
		return fmt.Errorf("%w: 기준일은 %s~%s 사이(YYYY-MM-DD): %q", ErrInvalid, AsOfMin, AsOfMax, asOf)
	}
	return nil
}

// Reset은 기준 세션을 비운다.
func (s *Service) Reset(ctx context.Context, asOf string) (View, error) {
	return s.ResetIn(ctx, store.MainSession, asOf)
}

// ResetIn은 세션의 사건 로그를 비우고, asOf가 있으면 그 기준일로 시작한다(E00_SESSION).
func (s *Service) ResetIn(ctx context.Context, sid, asOf string) (View, error) {
	if err := validAsOf(asOf); err != nil {
		return View{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reset(ctx, sid, asOf)
}

func (s *Service) reset(ctx context.Context, sid, asOf string) (View, error) {
	cs, err := s.load(ctx, sid)
	if err != nil {
		return View{}, err
	}
	if err := s.st.ResetEvents(ctx, cs.meta.ID); err != nil {
		return View{}, err
	}
	cs.events, cs.state = nil, newState(s.base, cs.meta.Kind)
	if asOf == "" || asOf == DefaultAsOf {
		return s.view(ctx, cs), nil
	}
	return s.submit(ctx, cs.meta.ID, EvSession, "시스템", json.RawMessage(fmt.Sprintf(`{"as_of":%q}`, asOf)))
}

// Inbox는 기준 세션의 접수함이다.
func (s *Service) Inbox(ctx context.Context, days, limit int) (Inbox, error) {
	return s.InboxIn(ctx, store.MainSession, days, limit)
}

// InboxIn은 세션의 접수함이다(최근 days일, 최대 limit건).
func (s *Service) InboxIn(ctx context.Context, sid string, days, limit int) (Inbox, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cs, err := s.load(ctx, sid)
	if err != nil {
		return Inbox{}, err
	}
	return cs.state.Inbox(s.base, days, limit), nil
}

// Backtest는 기준 세션의 정답 비교다.
func (s *Service) Backtest(ctx context.Context) (Backtest, error) {
	return s.BacktestIn(ctx, store.MainSession)
}

// BacktestIn은 세션의 정답 비교다.
func (s *Service) BacktestIn(ctx context.Context, sid string) (Backtest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cs, err := s.load(ctx, sid)
	if err != nil {
		return Backtest{}, err
	}
	return cs.state.Backtest(s.base), nil
}

// RunScenario는 기준 세션에서 시나리오를 돌린다.
func (s *Service) RunScenario(ctx context.Context, id, asOf string) (View, error) {
	return s.RunScenarioIn(ctx, store.MainSession, id, asOf)
}

// RunScenarioIn은 세션을 기준일 asOf(비면 그 세션 기준일)로 초기화한 뒤 시나리오의 사건을 고정 입력값으로 차례로 넣는다.
func (s *Service) RunScenarioIn(ctx context.Context, sid, id, asOf string) (View, error) {
	if err := validAsOf(asOf); err != nil {
		return View{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runScenario(ctx, sid, id, asOf)
}

func (s *Service) runScenario(ctx context.Context, sid, id, asOf string) (View, error) {
	cs, err := s.load(ctx, sid)
	if err != nil {
		return View{}, err
	}
	if asOf == "" {
		asOf = cs.state.Scenario.AsOf
	}
	sc, err := ScenarioByID(id, asOf)
	if err != nil {
		return View{}, err
	}
	if _, err := s.reset(ctx, sid, asOf); err != nil {
		return View{}, err
	}
	var v View
	for i, step := range sc.Steps {
		payload, err := stepJSON(step)
		if err != nil {
			return v, fmt.Errorf("시나리오 %s 단계 %d: %w", id, i+1, err)
		}
		v, err = s.submit(ctx, sid, step.Type, step.Actor, payload)
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
	rows := []Assumption{
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
		{"A-20", "기존 고객 납기·판가 변경 승인 = 영업 임원", "원본은 'C-101 재협상/판가 = 영업 임원'만 명시. 모든 기존 고객으로 일반화, 변경 흐름은 설계 단계", "가정", "변경 요청 승인 라우팅(R2·R14), CR-09 회신 권한(R13)"},
		{"A-21", "라인 보드 상태 문턱", "다음 달 부하 ÷ 배치 실적 월 생산: 100% 초과면 초과, 85% 이상이면 포화(표시용, 판단에 안 씀)", "가정", "생산 라인 보드 상태(여유·포화·초과)"},
		{"A-22", "이사회 = 기준일 + 5일(모든 기준일에 적용)", boardValue(sc.BoardDate), boardLabel, "결정 가능일, 승인 검토 작업 기한, OPT 결정일 기본값"},
		{"A-23", "이탈 경보 임계값(R12)", fmt.Sprintf("매출 비중 %.0f%% 이상 고객, 금액 기준. AT_RISK: 3개월 수주/출하 < %.2f 그리고 3개월 수주 YoY < %.0f%%. WATCH: 단월 수주/출하 < %.2f 2개월 연속. 출처가 정한 값 없음(설계값)", ChurnShareMin, ChurnRollingMax, ChurnYoYMax, ChurnWatchSingle), "가정", "판단 카드 이탈 경보, 영업 레인, CR-09 생성"},
		{"A-24", "이탈 의심 고객 능력 반환(R13)", churnReleaseValue(sc), lbl(sc.ChurnAnswered), "C-201 공급(출처 CUST-REL), PL-01 상한·PL-02 공급"},
	}
	if sc.FXShift != 0 { // 모의 가정 환율 충격(이슈 #39)
		rows[1].Value = fmt.Sprintf("%s %.2f %+.0f원(모의) = %.2f", at.FXDate, at.FX, sc.FXShift, at.FX+sc.FXShift)
		rows[1].Label = "모의 가정"
	}
	if len(sc.SimNotes) > 0 {
		rows = append(rows, Assumption{"SIM", "모의 가정(시뮬레이션 세션, E9)", strings.Join(sc.SimNotes, " · "), "모의 가정", "이 세션의 평가 전체(기준 세션에는 없음, 원본 제약은 그대로)"})
	}
	return rows
}

func churnReleaseValue(sc Scenario) string {
	switch {
	case !sc.ChurnAnswered:
		return "영업 임원 확인(CR-09) 전 반환 0 — 진행 주문은 '검토'로 표시, 보호 유지"
	case !sc.ChurnRevised:
		return "영업 임원 회신: 계약·예측 수정 아님 → 반환 0"
	default:
		return fmt.Sprintf("영업 임원 회신: 수정 확인 → 연 PL-01 %s · PL-02 %s EA 반환(월 ÷ 12)", Commas(sc.ChurnRelease[ProductBasic]), Commas(sc.ChurnRelease[ProductDerma]))
	}
}

// boardValue는 이사회일과 주말 처리 방식을 적는다. 원본(data_dictionary)은 "As-of + 5일"(달력일)과 영업일 월~금만 정하고
// 주말 이사회 규정은 없다(board_minutes·Business_Process_Map도 날짜 규칙 없음). 근거가 확인되기 전에는 보정하지 않는다.
func boardValue(date string) string {
	w := weekdayKo(date)
	if w == "토" || w == "일" {
		return fmt.Sprintf("%s(%s) · 주말이지만 보정 없음(근거 미확인: 원본에 주말 이사회 규정 없음)", date, w)
	}
	return fmt.Sprintf("%s(%s) · 주말 보정 없음(근거 미확인: 원본은 'As-of + 5일'만 정함)", date, w)
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
