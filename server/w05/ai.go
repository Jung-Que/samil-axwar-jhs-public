package w05

// AI 어댑터(D3: 결정 모델이 기본, 결정 모델로 처리할 수 없는 자유 문장만 API로 구조화한다).
//
// 서버 규칙(Go 표준 라이브러리 + pgx만)에 따라 공식 SDK 대신 Messages API를 HTTP로 직접 부른다.
// 키(ANTHROPIC_API_KEY)가 없으면 녹화 응답([모의])을 쓰고, 그것도 없으면 사람이 폼으로 입력한다.
// AI는 숫자를 계산하거나 승인하지 않는다. 출력은 형식 검증과 사람 확인을 거쳐 사건으로 기록된 뒤에야 엔진 입력이 된다.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// 구조화 종류(A1).
const (
	KindRFP      = "rfp"
	KindDecision = "decision"
	KindBuyer    = "buyer_reply"
)

const (
	defaultModel   = "claude-opus-5-5"
	apiVersion     = "2026-10-21"
	fallbackBeta   = "server-side-fallback-2026-07-01" // 거절(refusal) 시 서버가 다른 모델로 다시 돌린다
	defaultBaseURL = "https://api.anthropic.com"
)

// AiCall은 AI 호출 기록이다. 어떤 판단이 어떤 AI 출력에서 왔는지 추적한다.
type AiCall struct {
	ID           string          `json:"ai_call_id"`
	Purpose      string          `json:"purpose"` // A1
	Kind         string          `json:"kind"`
	Mode         string          `json:"mode"` // live | recorded | none
	Model        string          `json:"model"`
	InputSummary string          `json:"input_summary"`
	Output       json.RawMessage `json:"output,omitempty"`
	Validation   string          `json:"validation"` // pass | fail
	Error        string          `json:"error,omitempty"`
	HumanEdited  bool            `json:"human_edited"`
	At           string          `json:"at"`
}

// 외부 공개 앱이라 실제 호출을 제한한다(비용·남용 방지). 넘으면 폼 입력으로 넘긴다.
const (
	liveCallsPerMinute = 10
	maxInputRunes      = 8000
)

// AI는 구조화 호출기다.
type AI struct {
	key      string
	model    string
	baseURL  string
	client   *http.Client
	recorded map[string]json.RawMessage
	mu       sync.Mutex
	recent   []time.Time // 최근 1분 실제 호출 시각
}

// allow는 분당 실제 호출 한도 안이면 true다.
func (a *AI) allow(now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	keep := a.recent[:0]
	for _, t := range a.recent {
		if now.Sub(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	a.recent = keep
	if len(a.recent) >= liveCallsPerMinute {
		return false
	}
	a.recent = append(a.recent, now)
	return true
}

// NewAIFromEnv는 환경변수로 AI를 만든다. 키는 로그·응답에 남기지 않는다.
func NewAIFromEnv() *AI {
	a := &AI{key: os.Getenv("ANTHROPIC_API_KEY"), model: os.Getenv("ANTHROPIC_MODEL"), baseURL: os.Getenv("ANTHROPIC_BASE_URL"),
		client: &http.Client{Timeout: 90 * time.Second}, recorded: recordedResponses()}
	if a.model == "" {
		a.model = defaultModel
	}
	if a.baseURL == "" {
		a.baseURL = defaultBaseURL
	}
	return a
}

// Mode는 실제 호출 가능 여부다.
func (a *AI) Mode() string {
	if a.key != "" {
		return "live"
	}
	return "recorded"
}

// Model은 실제 호출에 쓰는 모델 이름이다.
func (a *AI) Model() string { return a.model }

// Structure는 자유 문장을 kind의 필드로 바꾼다. preferRecorded면 녹화 응답을 먼저 쓴다(시연 재현용).
func (a *AI) Structure(ctx context.Context, kind, text string, preferRecorded bool) *AiCall {
	call := &AiCall{Purpose: "A1", Kind: kind, InputSummary: summarize(text), At: time.Now().UTC().Format(time.RFC3339)}
	call.ID = "AI-" + textKey(kind, text+call.At)[:10]
	if _, ok := schemas[kind]; !ok {
		call.Mode, call.Validation, call.Error = "none", "fail", "모르는 구조화 종류: "+kind
		return call
	}
	if preferRecorded || a.key == "" {
		if out, ok := a.recorded[textKey(kind, text)]; ok {
			call.Mode, call.Model, call.Output = "recorded", "녹화 응답", out
			call.finish(validateFields(kind, out))
			return call
		}
		if a.key == "" {
			call.Mode, call.Validation, call.Error = "none", "fail", "API 키가 없고 이 문장의 녹화 응답도 없다 — 폼으로 입력한다"
			return call
		}
	}
	call.Mode, call.Model = "live", a.model
	if n := len([]rune(text)); n > maxInputRunes {
		call.Validation, call.Error = "fail", fmt.Sprintf("입력이 너무 길다(%d자 > %d자) — 줄이거나 폼으로 입력한다", n, maxInputRunes)
		return call
	}
	if !a.allow(time.Now()) {
		call.Validation, call.Error = "fail", fmt.Sprintf("AI 실제 호출 한도(분당 %d회)를 넘었다 — 잠시 뒤 다시 하거나 폼으로 입력한다", liveCallsPerMinute)
		return call
	}
	out, err := a.callAPI(ctx, kind, text)
	if err != nil {
		call.Validation, call.Error = "fail", err.Error()
		return call
	}
	call.Output = out
	call.finish(validateFields(kind, out))
	return call
}

func (c *AiCall) finish(err error) {
	if err != nil {
		c.Validation, c.Error = "fail", "형식 검증 실패: "+err.Error()
		return
	}
	c.Validation = "pass"
}

// callAPI는 Messages API에 구조화 출력(output_config.format)을 요청한다.
func (a *AI) callAPI(ctx context.Context, kind, text string) (json.RawMessage, error) {
	body := map[string]any{
		"model":      a.model,
		"max_tokens": 16000,
		"system":     systemPrompt + "\n\n" + kindGuide[kind],
		"messages":   []map[string]any{{"role": "user", "content": text}},
		"output_config": map[string]any{
			"effort": "low", // 단순 추출이라 낮은 effort
			"format": map[string]any{"type": "json_schema", "schema": schemas[kind]},
		},
		"fallbacks": "default",
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.baseURL, "/")+"/v1/messages", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", a.key)
	req.Header.Set("anthropic-version", apiVersion)
	req.Header.Set("anthropic-beta", fallbackBeta)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API 연결 실패: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		switch {
		case resp.StatusCode == http.StatusTooManyRequests:
			return nil, fmt.Errorf("API 호출 한도 초과(429): %s", e.Error.Message)
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			return nil, fmt.Errorf("API 인증 실패(%d): 키 설정을 확인", resp.StatusCode)
		case resp.StatusCode >= 500:
			return nil, fmt.Errorf("API 서버 오류(%d): %s", resp.StatusCode, e.Error.Message)
		default:
			return nil, fmt.Errorf("API 요청 오류(%d %s): %s", resp.StatusCode, e.Error.Type, e.Error.Message)
		}
	}
	var msg struct {
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("API 응답 해석 실패: %w", err)
	}
	switch msg.StopReason {
	case "refusal":
		return nil, errors.New("모델이 응답을 거절했다(refusal) — 폼으로 입력한다")
	case "max_tokens":
		return nil, errors.New("출력이 잘렸다(max_tokens)")
	}
	var text2 string
	for _, c := range msg.Content {
		if c.Type == "text" {
			text2 = c.Text
		}
	}
	if strings.TrimSpace(text2) == "" {
		return nil, errors.New("응답에 JSON 텍스트가 없다")
	}
	if !json.Valid([]byte(text2)) {
		return nil, errors.New("응답이 JSON이 아니다")
	}
	return json.RawMessage(text2), nil
}

// validateFields는 AI 출력이 사건 입력으로 쓸 수 있는 형식인지 결정 모델 규칙으로 검사한다.
func validateFields(kind string, out json.RawMessage) error {
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.DisallowUnknownFields()
	switch kind {
	case KindRFP:
		var p RFPFields
		if err := dec.Decode(&p); err != nil {
			return err
		}
		return validateItems(p.Items)
	case KindDecision:
		var p DecisionFields
		if err := dec.Decode(&p); err != nil {
			return err
		}
		return p.validate()
	case KindBuyer:
		var p BuyerReply
		if err := dec.Decode(&p); err != nil {
			return err
		}
		return p.validate()
	}
	return fmt.Errorf("모르는 종류 %s", kind)
}

// RFPFields는 RFP를 구조화한 필드다(E1 입력).
type RFPFields struct {
	Items         []RFPItem `json:"items"`
	Currency      string    `json:"currency"`
	PenaltyRate   float64   `json:"penalty_rate_monthly"`
	RecallBearer  string    `json:"recall_bearer"`
	DeadlineWeeks int       `json:"deadline_weeks"`
}

const systemPrompt = `너는 Corevia(화장품 OEM/ODM) 영업팀의 입력 도우미다. 주어진 문서나 회신에서 명시된 사실만 정해진 JSON 필드로 옮긴다.
- 적혀 있지 않은 값은 null로 둔다(선택지가 있으면 "언급없음"). 추정·계산·판단을 하지 않는다.
- 수량은 EA 정수, 단가는 숫자만 쓴다. 비율은 퍼센트 숫자로 쓴다(예: "7% 인하" → -7, "월 3%" → 0.03처럼 필드 설명을 따른다).
- 월은 YYYY-MM 형식으로 쓴다.
네 출력은 사람이 확인한 뒤 업무 시스템의 사건으로 기록된다. 숫자 계산과 승인은 시스템과 사람이 한다.`

var kindGuide = map[string]string{
	KindRFP: `종류: 입찰 RFP 요약.
- items: 제품군별 연 예상수량(annual_qty)과 제시단가(unit_price_usd). 제품군 코드는 문서의 PL-0n을 그대로 쓴다.
- penalty_rate_monthly: 납기 지연 페널티 월 비율(월 3%면 0.03). recall_bearer: 리콜 비용 부담(공급자 = supplier, 구매자 = buyer, 없으면 unknown).
- deadline_weeks: 입찰 마감까지 주 수.`,
	KindDecision: `종류: CEO/이사회의 입찰·투자 결정 회신.
- participation: 입찰 참여 결정(승인 | 조건부 | 거절). 조건을 붙여 승인하면 조건부.
- options: OPT-01(F1 더마 라인 증설, 180억)과 OPT-02(F2 더마 전환, 12억) 각각 승인 | 보류 | 거절 | 언급없음. "나중에 다시 올려라"는 보류.
- conditions: 회신에 담긴 조건에 맞는 코드만 고른다.
  COND_PL01_UNCONFIRMED = 기초(PL-01) 수량을 생산 확인 전 확정하지 말 것
  COND_NO_PRE_QUAL = 증설·전환 능력은 적격(재검증) 전 약속하지 말 것
  COND_OPT02_F2 = F2(해외공장) 물량은 바이어 생산지 수용 전 조건부
  COND_OPT01_STAGED = OPT-01 의존 물량은 OPT-01 승인 전 조건부
  COND_MASIL_PROTECTED = 계열사(Masil) 공급분을 건드리지 말 것
- price_floor_pct: 허용하는 단가 인하 한도(%, "-10%까지" → -10). 없으면 null.
- note: 회신 요지 한 문장.`,
	KindBuyer: `종류: 바이어(C-201)의 질의 회신.
- delivery_start: 납품(초도) 개시월 YYYY-MM. split_allowed: 분할 납품 허용 여부.
- price_cut_pct: 요구한 단가 인하율(%, 7% 인하 → -7). penalty_basis: 지연 페널티 기준(지연분 = backlog, 연 계약 총액 = contract).
- f2_acceptance: 해외공장(F2) 생산 수용 여부. initial_qty_required: 첫 달에 반드시 필요하다고 한 더마(PL-02) 수량. contract_months: 계약 기간(개월).`,
}

func nullable(t map[string]any) map[string]any {
	return map[string]any{"anyOf": []any{t, map[string]any{"type": "null"}}}
}

var schemas = map[string]map[string]any{
	KindRFP: {
		"type": "object", "additionalProperties": false,
		"required": []string{"items", "currency", "penalty_rate_monthly", "recall_bearer", "deadline_weeks"},
		"properties": map[string]any{
			"items": map[string]any{"type": "array", "items": map[string]any{
				"type": "object", "additionalProperties": false, "required": []string{"product_id", "annual_qty", "unit_price_usd"},
				"properties": map[string]any{
					"product_id":     map[string]any{"type": "string", "enum": []string{"PL-01", "PL-02", "PL-03", "PL-04", "PL-05", "PL-06"}},
					"annual_qty":     map[string]any{"type": "integer"},
					"unit_price_usd": map[string]any{"type": "number"},
				}}},
			"currency":             map[string]any{"type": "string"},
			"penalty_rate_monthly": map[string]any{"type": "number"},
			"recall_bearer":        map[string]any{"type": "string", "enum": []string{"supplier", "buyer", "unknown"}},
			"deadline_weeks":       map[string]any{"type": "integer"},
		},
	},
	KindDecision: {
		"type": "object", "additionalProperties": false,
		"required": []string{"participation", "options", "conditions", "price_floor_pct", "note"},
		"properties": map[string]any{
			"participation": map[string]any{"type": "string", "enum": []string{"승인", "조건부", "거절"}},
			"options": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"OPT-01", "OPT-02"},
				"properties": map[string]any{
					"OPT-01": map[string]any{"type": "string", "enum": []string{"승인", "보류", "거절", "언급없음"}},
					"OPT-02": map[string]any{"type": "string", "enum": []string{"승인", "보류", "거절", "언급없음"}},
				}},
			"conditions": map[string]any{"type": "array", "items": map[string]any{"type": "string",
				"enum": []string{"COND_PL01_UNCONFIRMED", "COND_NO_PRE_QUAL", "COND_OPT02_F2", "COND_OPT01_STAGED", "COND_MASIL_PROTECTED"}}},
			"price_floor_pct": nullable(map[string]any{"type": "number"}),
			"note":            map[string]any{"type": "string"},
		},
	},
	KindBuyer: {
		"type": "object", "additionalProperties": false,
		"required": []string{"delivery_start", "split_allowed", "price_cut_pct", "penalty_basis", "f2_acceptance", "initial_qty_required", "contract_months"},
		"properties": map[string]any{
			"delivery_start":       nullable(map[string]any{"type": "string"}),
			"split_allowed":        nullable(map[string]any{"type": "boolean"}),
			"price_cut_pct":        nullable(map[string]any{"type": "number"}),
			"penalty_basis":        nullable(map[string]any{"type": "string", "enum": []string{"backlog", "contract"}}),
			"f2_acceptance":        nullable(map[string]any{"type": "boolean"}),
			"initial_qty_required": nullable(map[string]any{"type": "integer"}),
			"contract_months":      nullable(map[string]any{"type": "integer"}),
		},
	},
}

func textKey(kind, text string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + strings.TrimSpace(text)))
	return hex.EncodeToString(sum[:])
}

func summarize(text string) string {
	t := strings.Join(strings.Fields(text), " ")
	r := []rune(t)
	if len(r) > 120 {
		return string(r[:120]) + "…"
	}
	return t
}
