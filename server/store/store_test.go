package store

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
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
