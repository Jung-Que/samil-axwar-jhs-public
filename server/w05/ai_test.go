package w05

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 실제 호출 경로: 요청 형식(헤더·구조화 출력·fallbacks)과 응답 해석·검증을 가짜 서버로 확인한다.
func TestStructureLive(t *testing.T) {
	var got map[string]any
	var hdr http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr = r.Header.Clone()
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		out := `{"delivery_start":"2026-11","split_allowed":false,"price_cut_pct":-5,"penalty_basis":"backlog","f2_acceptance":true,"initial_qty_required":null,"contract_months":24}`
		resp, _ := json.Marshal(map[string]any{
			"stop_reason": "end_turn",
			"content":     []map[string]any{{"type": "thinking", "thinking": ""}, {"type": "text", "text": out}},
		})
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write(resp)
	}))
	defer srv.Close()
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", srv.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	a := NewAIFromEnv()

	call := a.Structure(context.Background(), KindBuyer, "11월부터 납품, 분할 불가, 5% 인하, 해외공장 생산 수용, 2년 계약", false)
	if call.Mode != "live" || call.Validation != "pass" {
		t.Fatalf("live 호출 실패: %+v", call)
	}
	if hdr.Get("x-api-key") != "test-key" || hdr.Get("anthropic-version") != apiVersion || hdr.Get("anthropic-beta") != fallbackBeta {
		t.Fatalf("헤더 오류: %v", hdr)
	}
	if got["model"] != "claude-opus-5-5" || got["fallbacks"] != "default" {
		t.Fatalf("본문 오류: model=%v fallbacks=%v", got["model"], got["fallbacks"])
	}
	oc, _ := got["output_config"].(map[string]any)
	f, _ := oc["format"].(map[string]any)
	if oc["effort"] != "low" || f["type"] != "json_schema" || f["schema"] == nil {
		t.Fatalf("output_config 오류: %v", oc)
	}
	var br BuyerReply
	if err := json.Unmarshal(call.Output, &br); err != nil || br.ContractMonths == nil || *br.ContractMonths != 24 {
		t.Fatalf("출력 해석 오류: %s", call.Output)
	}
}

// 형식이 틀린 출력·거절·오류 상태는 통과시키지 않는다(사람 입력으로 넘김).
func TestStructureLiveFailures(t *testing.T) {
	cases := []struct {
		name, body string
		status     int
		want       string
	}{
		{"범위 밖 값", `{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"delivery_start\":\"내년\",\"split_allowed\":null,\"price_cut_pct\":null,\"penalty_basis\":null,\"f2_acceptance\":null,\"initial_qty_required\":null,\"contract_months\":null}"}]}`, 200, "형식 검증 실패"},
		{"거절", `{"stop_reason":"refusal","content":[]}`, 200, "refusal"},
		{"한도 초과", `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`, 429, "429"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()
			t.Setenv("ANTHROPIC_API_KEY", "k")
			t.Setenv("ANTHROPIC_BASE_URL", srv.URL)
			call := NewAIFromEnv().Structure(context.Background(), KindBuyer, "아무 회신", false)
			if call.Validation != "fail" || !strings.Contains(call.Error, c.want) {
				t.Fatalf("기대 실패(%s), 결과 %+v", c.want, call)
			}
		})
	}
}

// 외부 공개 앱 보호: 분당 실제 호출 한도와 입력 길이 제한.
func TestLiveCallLimits(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"delivery_start\":null,\"split_allowed\":null,\"price_cut_pct\":null,\"penalty_basis\":null,\"f2_acceptance\":null,\"initial_qty_required\":null,\"contract_months\":null}"}]}`))
	}))
	defer srv.Close()
	t.Setenv("ANTHROPIC_API_KEY", "k")
	t.Setenv("ANTHROPIC_BASE_URL", srv.URL)
	a := NewAIFromEnv()
	if c := a.Structure(context.Background(), KindBuyer, strings.Repeat("가", maxInputRunes+1), false); c.Validation != "fail" || calls != 0 {
		t.Fatalf("긴 입력이 호출됨: %+v", c)
	}
	for i := 0; i < liveCallsPerMinute; i++ {
		if c := a.Structure(context.Background(), KindBuyer, "회신", false); c.Validation != "pass" {
			t.Fatalf("%d번째 호출 실패: %+v", i+1, c)
		}
	}
	if c := a.Structure(context.Background(), KindBuyer, "회신", false); c.Validation != "fail" || !strings.Contains(c.Error, "한도") {
		t.Fatalf("한도를 넘었는데 호출됨: %+v", c)
	}
	if calls != liveCallsPerMinute {
		t.Fatalf("실제 호출 %d회, 기대 %d회", calls, liveCallsPerMinute)
	}
}

// 키가 없고 녹화 응답도 없으면 폼 입력으로 넘긴다.
func TestStructureNoKeyNoRecording(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	call := NewAIFromEnv().Structure(context.Background(), KindBuyer, "처음 보는 회신", false)
	if call.Mode != "none" || call.Validation != "fail" {
		t.Fatalf("%+v", call)
	}
}
