package store

import (
	"context"
	"sync"
	"time"
)

// memory는 DB 없이 도는 저장소다. 프로세스가 끝나면 상태가 사라진다.
type memory struct {
	mu       sync.Mutex
	sessions []Session
	events   map[string][]Event
}

// NewMemory는 메모리 저장소를 만든다. 스키마는 늘 최신으로 보고, 기준 세션(main)을 미리 둔다.
func NewMemory() Store {
	return &memory{
		sessions: []Session{{ID: MainSession, Name: "기준 세션", Kind: KindMain, CreatedAt: time.Now().UTC()}},
		events:   map[string][]Event{},
	}
}

func (*memory) Kind() string                               { return "memory" }
func (*memory) Ping(context.Context) error                 { return nil }
func (*memory) SchemaVersion(context.Context) (int, error) { return CurrentSchema, nil }
func (*memory) Close()                                     {}

func (m *memory) has(id string) bool {
	for _, s := range m.sessions {
		if s.ID == id {
			return true
		}
	}
	return false
}

func (m *memory) AppendEvent(_ context.Context, session string, seq int, typ string, body []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.has(session) {
		return ErrNoSession
	}
	for _, e := range m.events[session] {
		if e.Seq == seq {
			return ErrSeqTaken
		}
	}
	m.events[session] = append(m.events[session], Event{Seq: seq, Type: typ, Body: append([]byte(nil), body...), CreatedAt: time.Now().UTC()})
	return nil
}

func (m *memory) ListEvents(_ context.Context, session string) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.has(session) {
		return nil, ErrNoSession
	}
	return append([]Event(nil), m.events[session]...), nil
}

func (m *memory) ResetEvents(_ context.Context, session string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.has(session) {
		return ErrNoSession
	}
	delete(m.events, session)
	return nil
}

func (m *memory) ListSessions(context.Context) ([]Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Session(nil), m.sessions...), nil
}

func (m *memory) CreateSession(_ context.Context, s Session, events []Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.has(s.ID) {
		return ErrSessionExists
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now().UTC()
	}
	m.sessions = append(m.sessions, s)
	cp := make([]Event, len(events))
	for i, e := range events {
		e.Body = append([]byte(nil), e.Body...)
		cp[i] = e
	}
	m.events[s.ID] = cp
	return nil
}

func (m *memory) DeleteSession(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id == MainSession {
		return ErrMainSession
	}
	for i, s := range m.sessions {
		if s.ID == id {
			m.sessions = append(m.sessions[:i], m.sessions[i+1:]...)
			delete(m.events, id)
			return nil
		}
	}
	return ErrNoSession
}
