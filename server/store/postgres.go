package store

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// migrations는 순서대로 한 번씩 적용하는 스키마 변경이다. 이미 적용한 항목은 고치지 말고 뒤에 추가한다.
var migrations = []string{
	// 1: 앱 메타 정보. A05 §6 "사용 자료·가정·업무 설계의 버전 기록"을 여기에 남긴다.
	`CREATE TABLE app_meta (
		key        text PRIMARY KEY,
		value      text NOT NULL,
		updated_at timestamptz NOT NULL DEFAULT now()
	)`,
	// 2: W05 사건 로그. 상태는 이 로그를 순서대로 다시 적용해 만든다(순번 중복은 PK가 막는다).
	`CREATE TABLE w05_events (
		seq        integer PRIMARY KEY,
		type       text NOT NULL,
		body       jsonb NOT NULL,
		created_at timestamptz NOT NULL DEFAULT now()
	)`,
	// 3: 시뮬레이션 세션(이슈 #37). 사건 로그를 세션마다 나눈다. 기존 사건은 기준 세션(main)으로 옮긴다.
	// 서비스 단계에서는 main이 운영 세션이 되고, 시뮬레이션은 운영 세션에서 분기한다.
	`CREATE TABLE w05_sessions (
		id         text PRIMARY KEY,
		name       text NOT NULL,
		kind       text NOT NULL CHECK (kind IN ('main', 'sim')),
		parent_id  text,
		fork_seq   integer,
		created_at timestamptz NOT NULL DEFAULT now()
	);
	INSERT INTO w05_sessions (id, name, kind) VALUES ('main', '기준 세션', 'main');
	ALTER TABLE w05_events ADD COLUMN session_id text NOT NULL DEFAULT 'main' REFERENCES w05_sessions(id) ON DELETE CASCADE;
	ALTER TABLE w05_events DROP CONSTRAINT w05_events_pkey;
	ALTER TABLE w05_events ADD PRIMARY KEY (session_id, seq)`,
}

// CurrentSchema는 이 코드가 기대하는 스키마 버전이다.
var CurrentSchema = len(migrations)

// 여러 인스턴스가 동시에 떠도 스키마 변경은 하나씩 하도록 거는 잠금 번호(임의의 고정값).
const migrateLockID = 7_013_001

type postgres struct{ pool *pgxpool.Pool }

func openPostgres(ctx context.Context, cfg Config) (*postgres, error) {
	pcfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, errors.New("DATABASE_URL을 해석할 수 없다(값은 로그에 남기지 않는다)")
	}
	// AXHub의 DATABASE_URL은 트랜잭션 단위 풀러를 거친다. 준비된 문장은 연결이 바뀌면 깨지므로 simple protocol로 보낸다.
	pcfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("DB 풀 생성 실패: %w", err)
	}
	if err := waitReady(ctx, pool.Ping, cfg.Wait); err != nil {
		pool.Close()
		return nil, err
	}

	murl := cfg.MigrateURL
	if murl == "" {
		murl = cfg.DatabaseURL
	}
	if err := migrate(ctx, murl); err != nil {
		pool.Close()
		return nil, err
	}
	return &postgres{pool: pool}, nil
}

// waitReady는 DB가 응답할 때까지 ping을 되풀이한다. 컨테이너가 함께 뜰 때 DB가 늦게 열리는 경우를 받는다.
func waitReady(ctx context.Context, ping func(context.Context) error, wait time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	delay := 250 * time.Millisecond
	for attempt := 1; ; attempt++ {
		err := ping(ctx)
		if err == nil {
			return nil
		}
		if attempt == 1 || attempt%10 == 0 {
			log.Printf("store: DB 응답 대기 중(%d번째): %v", attempt, err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("DB가 %s 안에 응답하지 않음: %w", wait, err)
		case <-time.After(delay):
		}
		if delay < 2*time.Second {
			delay *= 2
		}
	}
}

// migrate는 적용하지 않은 스키마 변경을 한 트랜잭션에서 적용한다.
func migrate(ctx context.Context, url string) error {
	ccfg, err := pgx.ParseConfig(url)
	if err != nil {
		return errors.New("스키마 변경용 주소(DIRECT_DATABASE_URL, 없으면 DATABASE_URL)를 해석할 수 없다(값은 로그에 남기지 않는다)")
	}
	ccfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	conn, err := pgx.ConnectConfig(ctx, ccfg)
	if err != nil {
		return fmt.Errorf("스키마 변경용 연결 실패: %w", err)
	}
	defer conn.Close(context.Background())

	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background()) // 커밋한 뒤에는 아무 일도 하지 않는다

	steps := []string{
		fmt.Sprintf("SELECT pg_advisory_xact_lock(%d)", migrateLockID),
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version    integer PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`,
	}
	for _, s := range steps {
		if _, err := tx.Exec(ctx, s); err != nil {
			return fmt.Errorf("스키마 준비 실패: %w", err)
		}
	}

	var have int
	if err := tx.QueryRow(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&have); err != nil {
		return err
	}
	if have > CurrentSchema {
		return fmt.Errorf("DB 스키마(%d)가 코드(%d)보다 새롭다: 예전 버전으로 되돌린 배포인지 확인", have, CurrentSchema)
	}
	for v := have + 1; v <= CurrentSchema; v++ {
		if _, err := tx.Exec(ctx, migrations[v-1]); err != nil {
			return fmt.Errorf("스키마 %d 적용 실패: %w", v, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", v); err != nil {
			return err
		}
		log.Printf("store: 스키마 %d 적용", v)
	}
	return tx.Commit(ctx)
}

func (p *postgres) Kind() string                   { return "postgres" }
func (p *postgres) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }
func (p *postgres) Close()                         { p.pool.Close() }

func (p *postgres) SchemaVersion(ctx context.Context) (int, error) {
	var v int
	err := p.pool.QueryRow(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&v)
	return v, err
}

func (p *postgres) sessionExists(ctx context.Context, id string) error {
	var n int
	if err := p.pool.QueryRow(ctx, "SELECT count(*) FROM w05_sessions WHERE id = $1", id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrNoSession
	}
	return nil
}

// AppendEvent는 세션에 사건 한 줄을 넣는다. 같은 순번이 이미 있으면 ErrSeqTaken.
func (p *postgres) AppendEvent(ctx context.Context, session string, seq int, typ string, body []byte) error {
	if err := p.sessionExists(ctx, session); err != nil {
		return err
	}
	tag, err := p.pool.Exec(ctx,
		"INSERT INTO w05_events (session_id, seq, type, body) VALUES ($1, $2, $3, $4::jsonb) ON CONFLICT (session_id, seq) DO NOTHING",
		session, seq, typ, string(body))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrSeqTaken
	}
	return nil
}

func (p *postgres) ListEvents(ctx context.Context, session string) ([]Event, error) {
	if err := p.sessionExists(ctx, session); err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, "SELECT seq, type, body::text, created_at FROM w05_events WHERE session_id = $1 ORDER BY seq", session)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var body string
		if err := rows.Scan(&e.Seq, &e.Type, &body, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Body = []byte(body)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (p *postgres) ResetEvents(ctx context.Context, session string) error {
	if err := p.sessionExists(ctx, session); err != nil {
		return err
	}
	_, err := p.pool.Exec(ctx, "DELETE FROM w05_events WHERE session_id = $1", session)
	return err
}

func (p *postgres) ListSessions(ctx context.Context) ([]Session, error) {
	rows, err := p.pool.Query(ctx, "SELECT id, name, kind, COALESCE(parent_id, ''), COALESCE(fork_seq, 0), created_at FROM w05_sessions ORDER BY created_at, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.Name, &s.Kind, &s.ParentID, &s.ForkSeq, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CreateSession은 세션과 처음 사건들을 한 트랜잭션에서 넣는다(분기 도중 끊겨도 반쪽 세션이 남지 않게).
func (p *postgres) CreateSession(ctx context.Context, s Session, events []Event) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var parent any
	if s.ParentID != "" {
		parent = s.ParentID
	}
	tag, err := tx.Exec(ctx, "INSERT INTO w05_sessions (id, name, kind, parent_id, fork_seq) VALUES ($1, $2, $3, $4, $5) ON CONFLICT (id) DO NOTHING",
		s.ID, s.Name, s.Kind, parent, s.ForkSeq)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrSessionExists
	}
	for _, e := range events {
		if _, err := tx.Exec(ctx, "INSERT INTO w05_events (session_id, seq, type, body) VALUES ($1, $2, $3, $4::jsonb)", s.ID, e.Seq, e.Type, string(e.Body)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (p *postgres) DeleteSession(ctx context.Context, id string) error {
	if id == MainSession {
		return ErrMainSession
	}
	tag, err := p.pool.Exec(ctx, "DELETE FROM w05_sessions WHERE id = $1 AND kind <> 'main'", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNoSession
	}
	return nil
}
