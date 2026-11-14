// AX 인재전쟁 앱 서버 — Go 표준 라이브러리 + Postgres 드라이버(pgx)만 쓴다.
//
// 하는 일: 상태 확인(/healthz), W05 업무 API(/api/w05/*, api_w05.go), 빌드된 React 화면(WEB_DIR) 제공.
// axhub는 앱이 열 포트를 PORT 환경변수로, 앱 전용 DB 주소를 DATABASE_URL로 넣어 주고 /healthz로 상태를 확인한다.
// DATABASE_URL이 없으면 메모리 저장소로 뜬다(로컬 개발). 주소는 HOST(기본 127.0.0.1)에만 열린다.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"syscall"
	"time"

	"axwar/server/store"
	"axwar/server/w05"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 기본값은 server/ 안에서 실행할 때(go run .) 기준이다. 컨테이너는 WEB_DIR을 넣어 준다.
	webDir := envOr("WEB_DIR", filepath.Join("..", "web", "dist"))

	st, err := store.Open(ctx, store.FromEnv())
	if err != nil {
		log.Fatalf("저장소 열기 실패: %v", err)
	}
	defer st.Close()

	// 계정·권한 표(이슈 #55): 비어 있으면 기본 권한표와 시연 계정을 넣고, 표의 권한으로 판단한다.
	if err := setupAccess(ctx, st); err != nil {
		log.Fatalf("계정·권한 준비 실패: %v", err)
	}

	// W05 원본 복사본은 바이너리에 들어 있다. 해시가 다르면 뜨지 않는다(D4).
	base, err := w05.LoadBase()
	if err != nil {
		log.Fatalf("W05 원본 적재 실패: %v", err)
	}
	svc, err := w05.NewService(ctx, base, st)
	if err != nil {
		log.Fatalf("W05 서비스 시작 실패: %v", err)
	}
	log.Printf("W05: 원본 %d개 파일 해시 확인, 입력은 고정 필드(LLM 현재 제외)", base.Files)

	srv := &http.Server{
		Addr:              listenAddr(),
		Handler:           newHandler(webDir, st, svc),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	log.Printf("listening on http://%s (web: %s, store: %s)", srv.Addr, webDir, st.Kind())
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// listenAddr는 서버가 열 주소다. 로컬에서는 이 PC 안(127.0.0.1)에만 열고,
// 컨테이너(Dockerfile)는 HOST=0.0.0.0을 넣어 바깥(axhub 라우터)에서 들어오게 한다.
func listenAddr() string {
	return net.JoinHostPort(envOr("HOST", "127.0.0.1"), envOr("PORT", "8080"))
}

func newHandler(webDir string, st store.Store, svc *w05.Service) http.Handler {
	mux := http.NewServeMux()

	// 상태 확인 전용 경로: 로그인·DB 없이 바로 200을 준다. GET 패턴은 HEAD도 받는다.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})

	// 앱·저장소 상태. DB가 응답하지 않으면 503으로 알린다(/healthz는 그대로 200).
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		body := map[string]any{"app": "samil-axwar-jhs", "stage": "draft", "work": "W05", "store": st.Kind(), "db": "ok"}
		status := http.StatusOK
		v, err := st.SchemaVersion(ctx)
		if err == nil {
			err = st.Ping(ctx)
		}
		if err != nil {
			log.Printf("api/status: 저장소 오류: %v", err)
			body["db"] = "error"
			status = http.StatusServiceUnavailable
		} else {
			body["schema"] = v
		}
		writeJSON(w, status, body)
	})

	// 로그인(이슈 #55). W05 업무 API는 모두 로그인한 계정으로만 부른다.
	registerAuth(mux, st)
	w05mux := http.NewServeMux()
	registerW05(w05mux, svc)
	w05mux.HandleFunc("/api/w05/", notFoundJSON)
	mux.Handle("/api/w05/", requireLogin(st, w05mux))

	// 없는 API는 화면(index.html)이 아니라 JSON 404로 답한다.
	mux.HandleFunc("/api/", notFoundJSON)

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

func notFoundJSON(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "요청한 주소를 찾을 수 없습니다."})
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
