package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"axwar/server/w05"
)

// W05 업무 API. 상태는 사건 로그를 다시 적용해 만들고, 사건은 규칙에 맞을 때만 기록된다.
// 사건 로그는 세션마다 따로다(이슈 #37). 아래 엔드포인트의 ?session=은 없으면 기준 세션(main)이다.
//
//	GET    /api/w05/state                현재 상태(평가·제안·확인 요청·작업·결정·사건 로그, 세션 목록)
//	POST   /api/w05/events               사건 입력 {type, actor_role, payload} — payload는 고정 필드(자유 문장·LLM 현재 제외)
//	POST   /api/w05/reset                세션 초기화. 본문 {as_of}가 있으면 그 기준일로 시작
//	POST   /api/w05/scenarios/{id}       시연 시나리오 실행(normal | change | reject). 본문 {as_of, new_session}.
//	                                     new_session이면 새 시뮬레이션 세션에서 돌린다(기존 세션 보존)
//	GET    /api/w05/inbox?days=&limit=   접수함: 시계 날짜까지 최근 days일 접수(기본 7일, 최대 limit건)
//	GET    /api/w05/backtest             정답 비교: 기준일 추정 납기 대 그 뒤 실제 출하(검증 전용)
//	GET    /api/w05/sessions             세션 목록
//	POST   /api/w05/sessions             새 시뮬레이션 세션 {name, as_of}
//	POST   /api/w05/sessions/{id}/fork   분기 {seq, name}: 세션 id의 사건 1~seq를 복사한 새 시뮬레이션 세션
//	DELETE /api/w05/sessions/{id}        시뮬레이션 세션 삭제(main은 409)
//	GET    /api/w05/compare?a=&b=        두 세션 비교(KPI·제품×월·플래그·확인 요청·작업)
func registerW05(mux *http.ServeMux, svc *w05.Service) {
	unavailable := func(w http.ResponseWriter) bool {
		if svc == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "W05 서비스가 준비되지 않았다(저장소 확인)"})
			return true
		}
		return false
	}

	mux.HandleFunc("GET /api/w05/state", func(w http.ResponseWriter, r *http.Request) {
		if unavailable(w) {
			return
		}
		v, err := svc.SnapshotIn(r.Context(), sessionOf(r))
		respond(w, v, err)
	})

	mux.HandleFunc("GET /api/w05/sessions", func(w http.ResponseWriter, r *http.Request) {
		if unavailable(w) {
			return
		}
		ss, err := svc.Sessions(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, ss)
	})

	mux.HandleFunc("POST /api/w05/sessions", func(w http.ResponseWriter, r *http.Request) {
		if unavailable(w) {
			return
		}
		var in struct {
			Name string `json:"name"`
			AsOf string `json:"as_of"`
		}
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in)
		v, err := svc.NewSession(r.Context(), strings.TrimSpace(in.Name), strings.TrimSpace(in.AsOf))
		respond(w, v, err)
	})

	mux.HandleFunc("POST /api/w05/sessions/{id}/fork", func(w http.ResponseWriter, r *http.Request) {
		if unavailable(w) {
			return
		}
		var in struct {
			Seq  int    `json:"seq"`
			Name string `json:"name"`
		}
		if !decode(w, r, &in) {
			return
		}
		v, err := svc.Fork(r.Context(), r.PathValue("id"), in.Seq, strings.TrimSpace(in.Name))
		respond(w, v, err)
	})

	mux.HandleFunc("DELETE /api/w05/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		if unavailable(w) {
			return
		}
		if err := svc.DeleteSession(r.Context(), r.PathValue("id")); err != nil {
			writeErr(w, err)
			return
		}
		v, err := svc.Snapshot(r.Context())
		respond(w, v, err)
	})

	mux.HandleFunc("GET /api/w05/compare", func(w http.ResponseWriter, r *http.Request) {
		if unavailable(w) {
			return
		}
		q := r.URL.Query()
		c, err := svc.Compare(r.Context(), q.Get("a"), q.Get("b"))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, c)
	})

	mux.HandleFunc("POST /api/w05/events", func(w http.ResponseWriter, r *http.Request) {
		if unavailable(w) {
			return
		}
		var in struct {
			Type    string          `json:"type"`
			Actor   string          `json:"actor_role"`
			Payload json.RawMessage `json:"payload"`
		}
		if !decode(w, r, &in) {
			return
		}
		if strings.TrimSpace(in.Type) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "type이 필요하다"})
			return
		}
		v, err := svc.SubmitIn(r.Context(), sessionOf(r), in.Type, in.Actor, in.Payload)
		respond(w, v, err)
	})

	mux.HandleFunc("POST /api/w05/reset", func(w http.ResponseWriter, r *http.Request) {
		if unavailable(w) {
			return
		}
		in := optBody(r)
		v, err := svc.ResetIn(r.Context(), sessionOf(r), in.AsOf)
		respond(w, v, err)
	})

	mux.HandleFunc("POST /api/w05/scenarios/{id}", func(w http.ResponseWriter, r *http.Request) {
		if unavailable(w) {
			return
		}
		in := optBody(r)
		var v w05.View
		var err error
		if in.NewSession {
			v, err = svc.StartScenario(r.Context(), r.PathValue("id"), in.AsOf)
		} else {
			v, err = svc.RunScenarioIn(r.Context(), sessionOf(r), r.PathValue("id"), in.AsOf)
		}
		respond(w, v, err)
	})

	mux.HandleFunc("GET /api/w05/inbox", func(w http.ResponseWriter, r *http.Request) {
		if unavailable(w) {
			return
		}
		days := intParam(r, "days", 7, 1, 366)
		limit := intParam(r, "limit", 300, 1, 2000)
		in, err := svc.InboxIn(r.Context(), sessionOf(r), days, limit)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, in)
	})

	mux.HandleFunc("GET /api/w05/backtest", func(w http.ResponseWriter, r *http.Request) {
		if unavailable(w) {
			return
		}
		bt, err := svc.BacktestIn(r.Context(), sessionOf(r))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, bt)
	})
}

// sessionOf는 ?session= 값이다(없으면 ""= 기준 세션).
func sessionOf(r *http.Request) string { return strings.TrimSpace(r.URL.Query().Get("session")) }

type optIn struct {
	AsOf       string `json:"as_of"`
	NewSession bool   `json:"new_session"`
}

// optBody는 선택 본문 {"as_of":"YYYY-MM-DD","new_session":bool}을 읽는다(없으면 빈 값).
func optBody(r *http.Request) optIn {
	var in optIn
	_ = json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<16)).Decode(&in)
	in.AsOf = strings.TrimSpace(in.AsOf)
	return in
}

// writeErr는 오류 종류에 맞는 HTTP 상태로 답한다(없는 세션 404, 규칙 위반 409).
func writeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, w05.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, w05.ErrInvalid):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	default:
		log.Printf("api/w05: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}

func intParam(r *http.Request, key string, def, lo, hi int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return def
	}
	return min(max(v, lo), hi)
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON 형식 오류: " + err.Error()})
		return false
	}
	return true
}

// respond는 규칙 위반(ErrInvalid)이면 409와 함께 현재 상태를 돌려준다(화면이 바로 갱신되게).
func respond(w http.ResponseWriter, v w05.View, err error) {
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, v)
	case errors.Is(err, w05.ErrInvalid):
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "view": v})
	case errors.Is(err, w05.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
	default:
		log.Printf("api/w05: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
}
