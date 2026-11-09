package store

import "context"

// memory는 DB 없이 도는 저장소다. 프로세스가 끝나면 상태가 사라진다.
type memory struct{}

// NewMemory는 메모리 저장소를 만든다. 스키마는 늘 최신으로 본다.
func NewMemory() Store { return memory{} }

func (memory) Kind() string                               { return "memory" }
func (memory) Ping(context.Context) error                 { return nil }
func (memory) SchemaVersion(context.Context) (int, error) { return CurrentSchema, nil }
func (memory) Close()                                     {}
