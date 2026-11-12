package w05

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"axwar/server/store"
)

// 시뮬레이션 세션(이슈 #37). 세션은 이름 있는 사건 로그다. 분기는 어느 세션의 어느 사건 번호까지를 복사한 새 시뮬레이션 세션이다.
// 서비스 단계에서는 기준 세션(main)이 운영 세션이 되고, 시뮬레이션은 운영 세션의 지금 사건 번호에서 "지금부터 만약"으로 분기한다.

func newSessionID() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return "sim-" + strconv.FormatInt(time.Now().UnixMilli(), 36) + hex.EncodeToString(b)
}

// Sessions는 세션 목록이다.
func (s *Service) Sessions(ctx context.Context) ([]SessionInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	metas, err := s.st.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]SessionInfo, 0, len(metas))
	for _, m := range metas {
		n := -1
		if c := s.sess[m.ID]; c != nil {
			n = len(c.events)
		}
		out = append(out, sessionInfo(m, n))
	}
	return out, nil
}

func (s *Service) createSim(ctx context.Context, name, parent string, forkSeq int, events []Event) (*session, error) {
	meta := store.Session{ID: newSessionID(), Name: strings.TrimSpace(name), Kind: store.KindSim, ParentID: parent, ForkSeq: forkSeq}
	if meta.Name == "" {
		meta.Name = "시뮬레이션 " + time.Now().Format("01-02 15:04:05")
	}
	if len([]rune(meta.Name)) > 60 {
		meta.Name = string([]rune(meta.Name)[:60])
	}
	rows := make([]store.Event, 0, len(events))
	for _, e := range events {
		body, err := json.Marshal(e)
		if err != nil {
			return nil, err
		}
		rows = append(rows, store.Event{Seq: e.Seq, Type: e.Type, Body: body})
	}
	if err := s.st.CreateSession(ctx, meta, rows); err != nil {
		return nil, err
	}
	return s.load(ctx, meta.ID)
}

// NewSession은 빈 시뮬레이션 세션을 만들고, asOf가 기본 기준일이 아니면 그 기준일로 시작한다.
func (s *Service) NewSession(ctx context.Context, name, asOf string) (View, error) {
	if err := validAsOf(asOf); err != nil {
		return View{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if name == "" {
		name = "시뮬레이션 " + firstNonEmpty(asOf, DefaultAsOf)
	}
	cs, err := s.createSim(ctx, name, "", 0, nil)
	if err != nil {
		return View{}, err
	}
	return s.reset(ctx, cs.meta.ID, asOf)
}

// StartScenario는 새 시뮬레이션 세션을 만들어 시연 시나리오를 돌린다(기존 세션은 그대로 둔다).
func (s *Service) StartScenario(ctx context.Context, id, asOf string) (View, error) {
	if err := validAsOf(asOf); err != nil {
		return View{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	day := firstNonEmpty(asOf, DefaultAsOf)
	info, err := ScenarioByID(id, day)
	if err != nil {
		return View{}, err
	}
	cs, err := s.createSim(ctx, fmt.Sprintf("%s 시연 · %s", info.Title, day), "", 0, nil)
	if err != nil {
		return View{}, err
	}
	return s.runScenario(ctx, cs.meta.ID, id, day)
}

// Fork는 세션 sid의 사건 1~seq를 복사한 새 시뮬레이션 세션을 만든다(seq = 0이면 빈 세션, 사건 수면 전체).
func (s *Service) Fork(ctx context.Context, sid string, seq int, name string) (View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	src, err := s.load(ctx, sid)
	if err != nil {
		return View{}, err
	}
	if seq < 0 || seq > len(src.events) {
		return s.view(ctx, src), fmt.Errorf("%w: 분기 사건 번호는 0~%d", ErrInvalid, len(src.events))
	}
	if name == "" {
		name = fmt.Sprintf("%s 분기 @%d", src.meta.Name, seq)
	}
	cs, err := s.createSim(ctx, name, src.meta.ID, seq, src.events[:seq])
	if err != nil {
		return View{}, err
	}
	return s.view(ctx, cs), nil
}

// DeleteSession은 시뮬레이션 세션을 지운다. 기준 세션(main)은 지울 수 없다.
func (s *Service) DeleteSession(ctx context.Context, sid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.st.DeleteSession(ctx, sid)
	switch {
	case errors.Is(err, store.ErrMainSession):
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	case errors.Is(err, store.ErrNoSession):
		return fmt.Errorf("%w: %s", ErrNotFound, sid)
	case err != nil:
		return err
	}
	delete(s.sess, sid)
	return nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// ---- 세션 비교(S2) ----

// SessionKPI는 세션 한쪽의 핵심 수치다.
type SessionKPI struct {
	Session         SessionInfo `json:"session"`
	AsOf            string      `json:"as_of"`
	SimDate         string      `json:"sim_date"`
	BidState        string      `json:"bid_state"`
	BidVersion      int         `json:"bid_version"`
	PriceCut        float64     `json:"price_cut"`
	DeliveryStart   string      `json:"delivery_start"`
	PL01Status      string      `json:"pl01_status"`
	PL02Status      string      `json:"pl02_status"`
	PL01Schedule    int64       `json:"pl01_schedule"`
	PL02Schedule    int64       `json:"pl02_schedule"`
	PL02Coverage    float64     `json:"pl02_coverage_pct"`
	PenaltyBacklog  float64     `json:"penalty_backlog_krw"`
	PenaltyContract float64     `json:"penalty_contract_krw"`
	ContribSchedule float64     `json:"contrib_schedule_krw"`
	RevenueSchedule float64     `json:"revenue_schedule_krw"`
	Options         []string    `json:"options"` // "OPT-02 승인 · 2026-10~" 같은 요약
	OpenRequests    int         `json:"open_requests"`
	OpenTasks       int         `json:"open_tasks"`
	Reservations    int         `json:"reservations"`
}

// MonthDiff는 제품×월의 두 세션 값이다.
type MonthDiff struct {
	Month     string `json:"month"`
	AHas      bool   `json:"a_has"` // 그 세션의 계약 기간에 이 달이 있는지(없으면 화면에 "-")
	BHas      bool   `json:"b_has"`
	ASupply   int64  `json:"a_supply"`
	BSupply   int64  `json:"b_supply"`
	ASchedule int64  `json:"a_schedule"`
	BSchedule int64  `json:"b_schedule"`
	AShort    int64  `json:"a_short"`
	BShort    int64  `json:"b_short"`
}

// ItemDiff는 한쪽에만 있거나 상태가 다른 항목이다(플래그·확인 요청·작업).
type ItemDiff struct {
	Key  string `json:"key"`
	Text string `json:"text"`
	A    string `json:"a"` // 없으면 ""
	B    string `json:"b"`
}

// SessionCompare는 두 세션의 비교다.
type SessionCompare struct {
	A        SessionKPI             `json:"a"`
	B        SessionKPI             `json:"b"`
	Products map[string][]MonthDiff `json:"products"`
	Flags    []ItemDiff             `json:"flags"`
	Requests []ItemDiff             `json:"requests"`
	Tasks    []ItemDiff             `json:"tasks"`
}

// Compare는 두 세션을 비교한다.
func (s *Service) Compare(ctx context.Context, a, b string) (SessionCompare, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ca, err := s.load(ctx, a)
	if err != nil {
		return SessionCompare{}, err
	}
	cb, err := s.load(ctx, b)
	if err != nil {
		return SessionCompare{}, err
	}
	out := SessionCompare{A: kpiOf(ca), B: kpiOf(cb), Products: map[string][]MonthDiff{}}
	for _, p := range []string{ProductBasic, ProductDerma} {
		out.Products[p] = monthDiffs(ca.state.Eval.Products[p].Months, cb.state.Eval.Products[p].Months)
	}
	flagMap := func(st *State) map[string]string {
		m := map[string]string{}
		for _, f := range st.Eval.Flags {
			m[f.Code] = f.Level + " · " + f.Message
		}
		return m
	}
	out.Flags = itemDiffs(flagMap(ca.state), flagMap(cb.state), true)
	reqMap := func(st *State) map[string]string {
		m := map[string]string{}
		for _, r := range st.Requests {
			m[r.ID+" "+r.ToRole] = r.State
		}
		return m
	}
	out.Requests = itemDiffs(reqMap(ca.state), reqMap(cb.state), false)
	taskMap := func(st *State) map[string]string {
		m := map[string]string{}
		for _, t := range st.Tasks {
			m[t.Owner+" · "+t.Title] = t.State
		}
		return m
	}
	out.Tasks = itemDiffs(taskMap(ca.state), taskMap(cb.state), false)
	return out, nil
}

func kpiOf(cs *session) SessionKPI {
	st := cs.state
	ev := st.Eval
	k := SessionKPI{Session: sessionInfo(cs.meta, len(cs.events)), AsOf: st.Scenario.AsOf, SimDate: st.Scenario.SimDate, BidState: st.bidState(),
		PriceCut: st.Scenario.PriceCut, DeliveryStart: st.Scenario.DeliveryStart, PenaltyContract: ev.PenaltyContract, Reservations: len(st.Reservations)}
	if st.Bid != nil {
		k.BidVersion = st.Bid.Version
	}
	for _, l := range st.Lines {
		switch l.ProductID {
		case ProductBasic:
			k.PL01Status = l.QtyStatus
		case ProductDerma:
			k.PL02Status = l.QtyStatus
		}
	}
	p1, p2 := ev.Products[ProductBasic], ev.Products[ProductDerma]
	k.PL01Schedule, k.PL02Schedule, k.PL02Coverage = p1.ScheduleTotal, p2.ScheduleTotal, p2.CoveragePct
	k.PenaltyBacklog = p1.PenaltyBacklog + p2.PenaltyBacklog
	k.ContribSchedule = p1.ContribSchedule + p2.ContribSchedule
	k.RevenueSchedule = p1.RevenueSchedule + p2.RevenueSchedule
	for _, oe := range ev.Options {
		start := oe.StartMonth + "~"
		if !oe.Usable {
			start = "반영 안 함"
		}
		k.Options = append(k.Options, fmt.Sprintf("%s %s · %s", oe.OptionID, oe.Status, start))
	}
	for _, r := range st.Requests {
		if r.State != "반영" {
			k.OpenRequests++
		}
	}
	for _, t := range st.Tasks {
		if t.State == "열림" {
			k.OpenTasks++
		}
	}
	return k
}

func monthDiffs(a, b []MonthRow) []MonthDiff {
	idx := map[string]*MonthDiff{}
	for _, m := range a {
		idx[m.Month] = &MonthDiff{Month: m.Month, AHas: true, ASupply: m.Supply, ASchedule: m.Schedule, AShort: m.Short}
	}
	for _, m := range b {
		d := idx[m.Month]
		if d == nil {
			d = &MonthDiff{Month: m.Month}
			idx[m.Month] = d
		}
		d.BHas, d.BSupply, d.BSchedule, d.BShort = true, m.Supply, m.Schedule, m.Short
	}
	out := make([]MonthDiff, 0, len(idx))
	for _, d := range idx {
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Month < out[j].Month })
	return out
}

// itemDiffs는 두 맵에서 한쪽에만 있거나 값이 다른 항목을 낸다. textIsValue면 값 자체를 설명으로 쓴다(플래그).
func itemDiffs(a, b map[string]string, textIsValue bool) []ItemDiff {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	var out []ItemDiff
	for k := range keys {
		va, vb := a[k], b[k]
		if va == vb {
			continue
		}
		d := ItemDiff{Key: k, A: va, B: vb, Text: k}
		if textIsValue {
			d.Text = firstNonEmpty(vb, va)
			d.A, d.B = presence(va), presence(vb)
			if va != "" && vb != "" {
				d.B = "내용 바뀜"
			}
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func presence(v string) string {
	if v == "" {
		return ""
	}
	return "있음"
}
