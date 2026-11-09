package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"axwar/server/store"
	"axwar/server/w05"
)

// 빌드된 프런트 흉내: index.html과 번들 하나.
func fakeBuild(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte(`<div id="root"></div>`), 0o644))
	must(t, os.MkdirAll(filepath.Join(dir, "assets"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte(`console.log(1)`), 0o644))
	return dir
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func newTestService(t *testing.T, st store.Store) *w05.Service {
	t.Helper()
	base, err := w05.LoadBase()
	if err != nil {
		t.Fatal(err)
	}
	svc, err := w05.NewService(context.Background(), base, st)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestRoutes(t *testing.T) {
	st := store.NewMemory()
	h := newHandler(fakeBuild(t), st, newTestService(t, st))
	cases := []struct {
		name, method, path string
		status             int
		bodyHas            string
	}{
		{"상태 확인", "GET", "/healthz", 200, "ok"},
		{"상태 확인 HEAD", "HEAD", "/healthz", 200, ""},
		{"API 상태", "GET", "/api/status", 200, `"stage":"draft"`},
		{"W05 상태", "GET", "/api/w05/state", 200, `"raw_files_verified":15`},
		{"API 상태: 메모리 저장소", "GET", "/api/status", 200, `"store":"memory"`},
		{"API 상태: 스키마", "GET", "/api/status", 200, `"db":"ok"`},
		{"없는 API는 JSON 404", "GET", "/api/nope", 404, `"error":"not found"`},
		{"없는 API는 POST도 JSON 404", "POST", "/api/nope", 404, `"error":"not found"`},
		{"첫 화면", "GET", "/", 200, `id="root"`},
		{"화면 주소 새로고침", "GET", "/orders/C-201", 200, `id="root"`},
		{"정적 파일", "GET", "/assets/app.js", 200, "console.log"},
		{"없는 정적 파일", "GET", "/assets/gone.js", 404, ""},
		{"화면 경로에 POST", "POST", "/", 405, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
			if rec.Code != c.status {
				t.Fatalf("%s %s: status %d, want %d", c.method, c.path, rec.Code, c.status)
			}
			if c.bodyHas != "" && !strings.Contains(rec.Body.String(), c.bodyHas) {
				t.Fatalf("%s %s: body %q, want contains %q", c.method, c.path, rec.Body.String(), c.bodyHas)
			}
		})
	}
}

// W05 API: 시나리오 실행, 규칙에 맞지 않는 사건은 409로 거부되고 기록되지 않는다.
func TestW05API(t *testing.T) {
	st := store.NewMemory()
	h := newHandler(fakeBuild(t), st, newTestService(t, st))
	do := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec
	}
	if rec := do("POST", "/api/w05/scenarios/normal", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"state":"회신 대기"`) {
		t.Fatalf("정상 시나리오: %d %.300s", rec.Code, rec.Body.String())
	}
	evs, _ := st.ListEvents(context.Background())
	if len(evs) != 4 {
		t.Fatalf("사건 %d건, 기대 4건", len(evs))
	}
	// 회신 대기 상태에서 다시 제출하면 규칙 위반
	rec := do("POST", "/api/w05/events", `{"type":"E7_SUBMIT_PROPOSAL","actor_role":"영업","payload":{}}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "제출은 승인") {
		t.Fatalf("잘못된 제출: %d %.300s", rec.Code, rec.Body.String())
	}
	if evs, _ := st.ListEvents(context.Background()); len(evs) != 4 {
		t.Fatalf("거부된 사건이 기록됨: %d건", len(evs))
	}
	// 고정 필드 입력: 바이어 회신(단가 -7%) → 결정 낡음 → 재검토
	rec = do("POST", "/api/w05/events", `{"type":"E4_BUYER_REPLY","actor_role":"영업","payload":{"price_cut_pct":-7}}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"state":"재검토"`) {
		t.Fatalf("바이어 회신: %d %.300s", rec.Code, rec.Body.String())
	}
	// LLM은 현재 제외: 자유 문장 구조화 경로가 없다
	if rec := do("POST", "/api/w05/ai/structure", `{"kind":"buyer_reply","text":"회신"}`); rec.Code != 404 {
		t.Fatalf("AI 구조화 경로가 남아 있음: %d", rec.Code)
	}
	if rec := do("POST", "/api/w05/scenarios/nope", ""); rec.Code != 500 {
		t.Fatalf("없는 시나리오: %d", rec.Code)
	}
	if rec := do("POST", "/api/w05/reset", ""); rec.Code != 200 {
		t.Fatalf("초기화: %d", rec.Code)
	}
}

func TestNoBuild(t *testing.T) {
	h := newHandler(t.TempDir(), store.NewMemory(), nil) // index.html 없음
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503", rec.Code)
	}
	// 프런트가 없어도 배포 상태 확인은 살아 있어야 한다.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status %d, want 200", rec.Code)
	}
}

// DB가 응답하지 않는 저장소(예: 배포 중 DB 장애)
type downStore struct{}

func (downStore) Kind() string                               { return "postgres" }
func (downStore) Ping(context.Context) error                 { return errors.New("db down") }
func (downStore) SchemaVersion(context.Context) (int, error) { return 0, errors.New("db down") }
func (downStore) AppendEvent(context.Context, int, string, []byte) error {
	return errors.New("db down")
}
func (downStore) ListEvents(context.Context) ([]store.Event, error) {
	return nil, errors.New("db down")
}
func (downStore) ResetEvents(context.Context) error { return errors.New("db down") }
func (downStore) Close()                            {}

func TestStoreDown(t *testing.T) {
	h := newHandler(fakeBuild(t), downStore{}, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/status", nil))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"db":"error"`) {
		t.Fatalf("status %d body %q, want 503 db error", rec.Code, rec.Body.String())
	}
	// DB가 죽어도 배포 상태 확인(/healthz)은 살아 있어야 axhub가 앱을 내리지 않는다.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status %d, want 200", rec.Code)
	}
}

func TestListenAddr(t *testing.T) {
	t.Setenv("HOST", "")
	t.Setenv("PORT", "")
	if got := listenAddr(); got != "127.0.0.1:8080" {
		t.Fatalf("기본 주소 %q, want 127.0.0.1:8080 (로컬은 이 PC 안에만 연다)", got)
	}
	t.Setenv("HOST", "0.0.0.0")
	t.Setenv("PORT", "9090")
	if got := listenAddr(); got != "0.0.0.0:9090" {
		t.Fatalf("%q, want 0.0.0.0:9090", got)
	}
}
