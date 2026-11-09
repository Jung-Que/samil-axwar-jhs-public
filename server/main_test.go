package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestRoutes(t *testing.T) {
	h := newHandler(fakeBuild(t))
	cases := []struct {
		name, method, path string
		status             int
		bodyHas            string
	}{
		{"상태 확인", "GET", "/healthz", 200, "ok"},
		{"상태 확인 HEAD", "HEAD", "/healthz", 200, ""},
		{"API 상태", "GET", "/api/status", 200, `"stage":"skeleton"`},
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

func TestNoBuild(t *testing.T) {
	h := newHandler(t.TempDir()) // index.html 없음
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
