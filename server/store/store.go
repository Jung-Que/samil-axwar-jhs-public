// Package store는 앱 상태를 어디에 저장하는지 감춘다.
//
// DATABASE_URL이 있으면 Postgres(AXHub 배포·로컬 Docker dev 테스트), 없으면 메모리(로컬 개발·단위 테스트)를 쓴다.
// 업무 상태는 사건 로그(Event)만 저장하고, 상태는 로그를 순서대로 다시 적용해 만든다(이벤트 소싱).
package store

import (
	"context"
	"errors"
	"os"
	"time"
)

// Event는 업무 사건 로그 한 줄이다. Body는 사건 내용(JSON)이다.
type Event struct {
	Seq       int
	Type      string
	Body      []byte
	CreatedAt time.Time
}

// ErrSeqTaken은 같은 순번의 사건이 이미 있을 때 돌려준다(동시에 두 사건을 쓰려 할 때).
var ErrSeqTaken = errors.New("사건 순번이 이미 있다")

// Store는 서버가 쓰는 저장소다.
type Store interface {
	Kind() string // "memory" 또는 "postgres"
	Ping(ctx context.Context) error
	SchemaVersion(ctx context.Context) (int, error)
	AppendEvent(ctx context.Context, seq int, typ string, body []byte) error
	ListEvents(ctx context.Context) ([]Event, error)
	ResetEvents(ctx context.Context) error
	Close()
}

// Config는 저장소 연결 설정이다. 비밀값(접속 주소)은 로그에 남기지 않는다.
type Config struct {
	DatabaseURL string        // 비어 있으면 메모리 저장소. AXHub는 트랜잭션 풀러를 거치는 주소를 넣는다
	MigrateURL  string        // 스키마 변경용(AXHub DIRECT_DATABASE_URL). 비어 있으면 DatabaseURL
	Wait        time.Duration // 시작할 때 DB가 응답할 때까지 기다리는 최대 시간
}

// FromEnv는 환경변수에서 설정을 읽는다. DB_WAIT은 Go 기간 형식(예: 45s)이다.
func FromEnv() Config {
	wait := 30 * time.Second
	if v := os.Getenv("DB_WAIT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			wait = d
		}
	}
	return Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		MigrateURL:  os.Getenv("DIRECT_DATABASE_URL"),
		Wait:        wait,
	}
}

// Open은 설정에 맞는 저장소를 연다. Postgres는 응답을 기다린 뒤 스키마를 최신으로 맞춘다.
func Open(ctx context.Context, cfg Config) (Store, error) {
	if cfg.DatabaseURL == "" {
		return NewMemory(), nil
	}
	return openPostgres(ctx, cfg)
}
