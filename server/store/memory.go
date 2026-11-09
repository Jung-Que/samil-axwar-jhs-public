package store

import (
	"context"
	"sync"
	"time"
)

// memory는 DB 없이 도는 저장소다. 프로세스가 끝나면 상태가 사라진다.
type memory struct {
	mu     sync.Mutex
	events []Event
}

// NewMemory는 메모리 저장소를 만든다. 스키마는 늘 최신으로 본다.
func NewMemory() Store { return &memory{} }

func (*memory) Kind() string                               { return "memory" }
func (*memory) Ping(context.Context) error                 { return nil }
func (*memory) SchemaVersion(context.Context) (int, error) { return CurrentSchema, nil }
func (*memory) Close()                                     {}

func (m *memory) AppendEvent(_ context.Context, seq int, typ string, body []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.events {
		if e.Seq == seq {
			return ErrSeqTaken
		}
	}
	m.events = append(m.events, Event{Seq: seq, Type: typ, Body: append([]byte(nil), body...), CreatedAt: time.Now().UTC()})
	return nil
}

func (m *memory) ListEvents(context.Context) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Event(nil), m.events...), nil
}

func (m *memory) ResetEvents(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = nil
	return nil
}
