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
//
//	GET  /api/w05/state              현재 상태(평가·제안·확인 요청·작업·결정·사건 로그)
//	POST /api/w05/events             사건 입력 {type, actor_role, payload} — payload는 고정 필드(자유 문장·LLM 현재 제외)
//	POST /api/w05/reset              사건 로그 초기화. 본문 {as_of}가 있으면 그 기준일로 테스트 세션 시작
//	POST /api/w05/scenarios/{id}     초기화 뒤 시연 시나리오 실행(normal | change | reject). 본문 {as_of} 선택
//	GET  /api/w05/inbox?days=&limit= 접수함: 시계 날짜까지 최근 days일 접수(기본 7일, 최대 limit건)
//	GET  /api/w05/backtest           정답 비교: 기준일 추정 납기 대 그 뒤 실제 출하(검증 전용)
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
		v, err := svc.Snapshot(r.Context())
		respond(w, v, err)
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
		v, err := svc.Submit(r.Context(), in.Type, in.Actor, in.Payload)
		respond(w, v, err)
	})

	mux.HandleFunc("POST /api/w05/reset", func(w http.ResponseWriter, r *http.Request) {
		if unavailable(w) {
			return
		}
		v, err := svc.Reset(r.Context(), asOfBody(r))
		respond(w, v, err)
	})

	mux.HandleFunc("POST /api/w05/scenarios/{id}", func(w http.ResponseWriter, r *http.Request) {
		if unavailable(w) {
			return
		}
		v, err := svc.RunScenario(r.Context(), r.PathValue("id"), asOfBody(r))
		respond(w, v, err)
	})

	mux.HandleFunc("GET /api/w05/inbox", func(w http.ResponseWriter, r *http.Request) {
		if unavailable(w) {
			return
		}
		days := intParam(r, "days", 7, 1, 366)
		limit := intParam(r, "limit", 300, 1, 2000)
		in, err := svc.Inbox(r.Context(), days, limit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, in)
	})

	mux.HandleFunc("GET /api/w05/backtest", func(w http.ResponseWriter, r *http.Request) {
		if unavailable(w) {
			return
		}
		bt, err := svc.Backtest(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, bt)
	})
}

// asOfBody는 선택 본문 {"as_of":"YYYY-MM-DD"}를 읽는다(없거나 비면 "").
func asOfBody(r *http.Request) string {
	var in struct {
		AsOf string `json:"as_of"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<16)).Decode(&in)
	return strings.TrimSpace(in.AsOf)
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
	default:
		log.Printf("api/w05: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
}
