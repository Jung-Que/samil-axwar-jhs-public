// AX 인재전쟁 앱 서버 — Go 표준 라이브러리만 쓴다.
//
// 하는 일: 상태 확인(/healthz), 업무 API(/api/*), 빌드된 React 화면(WEB_DIR) 제공.
// axhub는 앱이 열 포트를 PORT 환경변수로 넣어 주고 /healthz로 상태를 확인한다.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"time"
)

func main() {
	port := envOr("PORT", "8080")
	// 기본값은 server/ 안에서 실행할 때(go run .) 기준이다. 컨테이너는 WEB_DIR을 넣어 준다.
	webDir := envOr("WEB_DIR", filepath.Join("..", "web", "dist"))

	srv := &http.Server{
		Addr:              "0.0.0.0:" + port,
		Handler:           newHandler(webDir),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("listening on http://%s (web: %s)", srv.Addr, webDir)
	log.Fatal(srv.ListenAndServe())
}

func newHandler(webDir string) http.Handler {
	mux := http.NewServeMux()

	// 상태 확인 전용 경로: 로그인·DB 없이 바로 200을 준다. GET 패턴은 HEAD도 받는다.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"app": "samil-axwar-jhs", "stage": "skeleton"})
	})

	// 없는 API는 화면(index.html)이 아니라 JSON 404로 답한다.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	})

	mux.Handle("/", spaHandler(webDir))
	return mux
}

// spaHandler는 빌드된 파일을 그대로 주고, 확장자 없는 경로(화면 주소)는 index.html로 돌린다.
// 그래야 /orders/1 같은 화면 주소에서 새로고침해도 React가 화면을 그린다.
func spaHandler(webDir string) http.Handler {
	files := http.FileServer(http.Dir(webDir))
	index := filepath.Join(webDir, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if _, err := os.Stat(index); err != nil {
			http.Error(w, "프런트 빌드가 없습니다: cd web && npm run build", http.StatusServiceUnavailable)
			return
		}

		p := path.Clean("/" + r.URL.Path)
		if p != "/" {
			if fi, err := os.Stat(filepath.Join(webDir, filepath.FromSlash(p))); err == nil && !fi.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
			if path.Ext(p) != "" { // 없는 정적 파일(예: 지운 번들)은 화면 대신 404
				http.NotFound(w, r)
				return
			}
		}
		http.ServeFile(w, r, index)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
