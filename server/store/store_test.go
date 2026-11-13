package store

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestOpenWithoutURLIsMemory(t *testing.T) {
	st, err := Open(context.Background(), Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if st.Kind() != "memory" {
		t.Fatalf("kind %q, want memory", st.Kind())
	}
	if err := st.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	if v, _ := st.SchemaVersion(context.Background()); v != CurrentSchema {
		t.Fatalf("schema %d, want %d", v, CurrentSchema)
	}
}

// 사건 로그: 순서대로 읽히고, 같은 순번은 거부되고, 초기화하면 비어야 한다(메모리·Postgres 공통).
// 세션: 분기 세션은 처음 사건을 복사해 독립으로 쌓이고, main은 지울 수 없다(이슈 #37).
func checkEvents(t *testing.T, st Store) {
	t.Helper()
	ctx := context.Background()
	if err := st.ResetEvents(ctx, MainSession); err != nil {
		t.Fatal(err)
	}
	for i, typ := range []string{"E1_RFP", "E8_SUBMIT_APPROVAL"} {
		if err := st.AppendEvent(ctx, MainSession, i+1, typ, []byte(`{"k":"값"}`)); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.AppendEvent(ctx, MainSession, 2, "E2_DECISION", []byte(`{}`)); !errors.Is(err, ErrSeqTaken) {
		t.Fatalf("중복 순번이 거부되지 않음: %v", err)
	}
	evs, err := st.ListEvents(ctx, MainSession)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 || evs[0].Seq != 1 || evs[1].Type != "E8_SUBMIT_APPROVAL" || !strings.Contains(string(evs[0].Body), "값") {
		t.Fatalf("사건 로그 왕복 실패: %+v", evs)
	}

	// 분기: main의 사건 1번까지 복사한 시뮬레이션 세션
	sid := "sim-test-" + time.Now().Format("150405.000000")
	if err := st.CreateSession(ctx, Session{ID: sid, Name: "분기 시험", Kind: KindSim, ParentID: MainSession, ForkSeq: 1}, evs[:1]); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(ctx, Session{ID: sid, Name: "중복", Kind: KindSim}, nil); !errors.Is(err, ErrSessionExists) {
		t.Fatalf("같은 ID 세션이 거부되지 않음: %v", err)
	}
	if err := st.AppendEvent(ctx, sid, 2, "E2_DECISION", []byte(`{"분기":true}`)); err != nil {
		t.Fatalf("분기 세션에 같은 순번(2) 사건이 들어가야 함: %v", err)
	}
	fe, _ := st.ListEvents(ctx, sid)
	me, _ := st.ListEvents(ctx, MainSession)
	if len(fe) != 2 || fe[1].Type != "E2_DECISION" || len(me) != 2 || me[1].Type != "E8_SUBMIT_APPROVAL" {
		t.Fatalf("분기 세션이 독립이 아님: 분기 %+v / main %+v", fe, me)
	}
	ss, _ := st.ListSessions(ctx)
	found := false
	for _, s := range ss {
		if s.ID == sid && s.Kind == KindSim && s.ParentID == MainSession && s.ForkSeq == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("세션 목록에 분기 세션 정보 없음: %+v", ss)
	}
	if err := st.DeleteSession(ctx, MainSession); !errors.Is(err, ErrMainSession) {
		t.Fatalf("main 삭제가 거부되지 않음: %v", err)
	}
	if err := st.DeleteSession(ctx, sid); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ListEvents(ctx, sid); !errors.Is(err, ErrNoSession) {
		t.Fatalf("지운 세션의 사건을 읽음: %v", err)
	}

	if err := st.ResetEvents(ctx, MainSession); err != nil {
		t.Fatal(err)
	}
	if evs, _ := st.ListEvents(ctx, MainSession); len(evs) != 0 {
		t.Fatalf("초기화 후 %d건 남음", len(evs))
	}
}

func TestMemoryEvents(t *testing.T) { checkEvents(t, NewMemory()) }

func TestFromEnv(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://a@h/db")
	t.Setenv("DIRECT_DATABASE_URL", "postgres://a@h2/db")
	t.Setenv("DB_WAIT", "5s")
	c := FromEnv()
	if c.DatabaseURL != "postgres://a@h/db" || c.MigrateURL != "postgres://a@h2/db" || c.Wait != 5*time.Second {
		t.Fatalf("got %+v", c)
	}
	t.Setenv("DB_WAIT", "엉터리")
	if FromEnv().Wait != 30*time.Second {
		t.Fatal("잘못된 DB_WAIT은 기본값 30s로")
	}
}

// DB가 없는 주소는 기다림 시간 안에 오류로 끝나야 한다(배포가 멈춘 채 떠 있지 않게).
func TestOpenUnreachableFailsWithinWait(t *testing.T) {
	start := time.Now()
	_, err := Open(context.Background(), Config{
		DatabaseURL: "postgres://nobody:secret-pw@127.0.0.1:1/none?sslmode=disable&connect_timeout=1",
		Wait:        time.Second,
	})
	if err == nil {
		t.Fatal("연결할 수 없는 주소인데 성공함")
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Fatalf("%s 걸림, 기다림 시간을 지키지 않음", took)
	}
	if strings.Contains(err.Error(), "secret-pw") {
		t.Fatalf("오류 메시지에 비밀번호가 드러남: %v", err)
	}
}

func TestOpenBadURLHidesValue(t *testing.T) {
	_, err := Open(context.Background(), Config{DatabaseURL: "postgres://u:secret-pw@[bad", Wait: time.Second})
	if err == nil || strings.Contains(err.Error(), "secret-pw") {
		t.Fatalf("형식 오류는 값 없이 실패해야 함: %v", err)
	}
}

func TestWaitReadyRetries(t *testing.T) {
	calls := 0
	ping := func(context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("아직")
		}
		return nil
	}
	if err := waitReady(context.Background(), ping, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("ping %d번, want 3", calls)
	}
}

// 아래는 실제 Postgres가 있을 때만 돈다(로컬 Docker dev 테스트: python tools/selftest.py devdb).
func pgURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("TEST_DATABASE_URL")
	if u == "" {
		t.Skip("TEST_DATABASE_URL 없음 — Postgres 통합 테스트는 python tools/selftest.py devdb 로 돌린다")
	}
	return u
}

func TestPostgresOpenMigratesIdempotently(t *testing.T) {
	url := pgURL(t)
	ctx := context.Background()
	cfg := Config{DatabaseURL: url, Wait: 30 * time.Second}

	// 동시에 여러 인스턴스가 떠도 스키마 변경은 한 번만 적용되어야 한다.
	var wg sync.WaitGroup
	errs := make([]error, 3)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			st, err := Open(ctx, cfg)
			if err == nil {
				st.Close()
			}
			errs[i] = err
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	st, err := Open(ctx, cfg) // 이미 최신이면 아무것도 바꾸지 않는다
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if st.Kind() != "postgres" {
		t.Fatalf("kind %q", st.Kind())
	}
	if err := st.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	v, err := st.SchemaVersion(ctx)
	if err != nil || v != CurrentSchema {
		t.Fatalf("schema %d (%v), want %d", v, err, CurrentSchema)
	}

	pool := st.(*postgres).pool
	var applied int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != CurrentSchema {
		t.Fatalf("schema_migrations %d행, want %d (중복 적용)", applied, CurrentSchema)
	}

	// 쓰기·읽기 왕복(app_meta)
	if _, err := pool.Exec(ctx, `INSERT INTO app_meta (key, value) VALUES ('selftest', 'ok')
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := pool.QueryRow(ctx, "SELECT value FROM app_meta WHERE key = 'selftest'").Scan(&got); err != nil || got != "ok" {
		t.Fatalf("app_meta 왕복 실패: %q %v", got, err)
	}
	checkEvents(t, st)
}

// 마이그레이션 3(세션)은 스키마 2에 쌓여 있던 사건을 기준 세션(main)으로 보존해야 한다(이슈 #37).
func TestPostgresMigration3KeepsEvents(t *testing.T) {
	url := pgURL(t)
	ctx := context.Background()
	ccfg, err := pgx.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	ccfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	conn, err := pgx.ConnectConfig(ctx, ccfg)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	// 스키마 2 상태를 만든다: 이전 마이그레이션 1·2만 적용하고 사건 두 건을 넣는다
	setup := []string{
		"DROP TABLE IF EXISTS app_logins, app_users, app_role_permissions, app_roles, w05_events, w05_sessions, app_meta, schema_migrations CASCADE",
		migrations[0], migrations[1],
		"CREATE TABLE schema_migrations (version integer PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())",
		"INSERT INTO schema_migrations (version) VALUES (1), (2)",
		`INSERT INTO w05_events (seq, type, body) VALUES (1, 'E1_RFP', '{"k":"값"}'), (2, 'E8_SUBMIT_APPROVAL', '{}')`,
	}
	for _, s := range setup {
		if _, err := conn.Exec(ctx, s); err != nil {
			t.Fatalf("스키마 2 준비 실패(%.40s): %v", s, err)
		}
	}
	st, err := Open(ctx, Config{DatabaseURL: url, Wait: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if v, _ := st.SchemaVersion(ctx); v != CurrentSchema {
		t.Fatalf("schema %d, want %d", v, CurrentSchema)
	}
	evs, err := st.ListEvents(ctx, MainSession)
	if err != nil || len(evs) != 2 || evs[0].Type != "E1_RFP" || !strings.Contains(string(evs[0].Body), "값") {
		t.Fatalf("마이그레이션 뒤 기준 세션 사건: %+v (%v)", evs, err)
	}
	ss, _ := st.ListSessions(ctx)
	if len(ss) != 1 || ss[0].ID != MainSession || ss[0].Kind != KindMain {
		t.Fatalf("마이그레이션 뒤 세션: %+v", ss)
	}
	checkEvents(t, st) // 세션별 쓰기·분기도 그대로 돈다
}

func TestPostgresRejectsNewerSchema(t *testing.T) {
	url := pgURL(t)
	ctx := context.Background()
	st, err := Open(ctx, Config{DatabaseURL: url, Wait: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	pool := st.(*postgres).pool
	future := CurrentSchema + 100
	if _, err := pool.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", future); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM schema_migrations WHERE version = $1", future)
		st.Close()
	})

	if _, err := Open(ctx, Config{DatabaseURL: url, Wait: 30 * time.Second}); err == nil {
		t.Fatal("코드보다 새 스키마인데 열림")
	}
}
