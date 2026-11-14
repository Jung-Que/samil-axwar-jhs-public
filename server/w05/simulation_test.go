package w05

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"axwar/server/store"
)

// 이슈 #39: 모의 가정 사건(E9)과 민감도 격자.

func contribRequest(ev *Evaluation) float64 {
	return ev.Products[ProductBasic].ContribRequest + ev.Products[ProductDerma].ContribRequest
}

func TestSimAssumptionOnlyInSimSession(t *testing.T) {
	ctx := context.Background()
	b := loadBase(t)
	svc, _ := NewService(ctx, b, store.NewMemory())
	mv, err := svc.RunScenario(ctx, "normal", "")
	if err != nil {
		t.Fatal(err)
	}
	// 기준 세션: 거절, 기록 안 함
	if _, err := svc.Submit(ctx, EvSimAssumption, "시뮬레이터", []byte(`{"fx_shift":-100}`)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("기준 세션 E9가 거절되지 않았다: %v", err)
	}
	if v, _ := svc.Snapshot(ctx); len(v.State.Log) != 4 || v.State.Scenario.FXShift != 0 {
		t.Fatalf("거절된 E9가 기준 세션을 바꿨다: 사건 %d, 충격 %v", len(v.State.Log), v.State.Scenario.FXShift)
	}

	// 시뮬레이션 세션: 환율 −100원 → 공헌이익이 엔진 환율 민감도(fx_down100_contrib)만큼 준다
	fv, err := svc.Fork(ctx, store.MainSession, len(mv.State.Log), "환율 충격")
	if err != nil {
		t.Fatal(err)
	}
	before := fv.State.Eval
	fv, err = svc.SubmitIn(ctx, fv.Session.ID, EvSimAssumption, "시뮬레이터", []byte(`{"fx_shift":-100}`))
	if err != nil {
		t.Fatal(err)
	}
	after := fv.State.Eval
	if d := contribRequest(before) - contribRequest(after); math.Abs(d-before.FXDown100Contr) > 1 {
		t.Errorf("환율 −100원 공헌이익 감소 %.0f, 엔진 민감도 %.0f", d, before.FXDown100Contr)
	}
	if math.Abs(after.FX-(before.FX-100)) > 1e-9 || after.FXShift != -100 {
		t.Errorf("평가 환율 %.2f(충격 %v), 기대 %.2f", after.FX, after.FXShift, before.FX-100)
	}
	last := fv.State.Log[len(fv.State.Log)-1]
	if last.Event.Type != EvSimAssumption || !strings.Contains(strings.Join(last.Changes, " "), "[모의 가정] 환율 충격 -100원") {
		t.Errorf("로그에 [모의 가정] 없음: %v", last.Changes)
	}
	found := false
	for _, a := range fv.Assumptions {
		if a.ID == "SIM" && a.Label == "모의 가정" && strings.Contains(a.Value, "환율 충격") {
			found = true
		}
		if a.ID == "A-02" && a.Label != "모의 가정" {
			t.Errorf("A-02 환율 성격 %q, 기대 모의 가정", a.Label)
		}
	}
	if !found {
		t.Error("가정 표에 SIM(모의 가정) 행이 없다")
	}

	// 증설안 결정·적격 주수 모의 가정: OPT-01 승인 + 0주 → 산입이 설치 다음 달로
	fv, err = svc.SubmitIn(ctx, fv.Session.ID, EvSimAssumption, "시뮬레이터", []byte(`{"options":{"OPT-01":"승인"},"validation_weeks":{"OPT-01":0}}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, oe := range fv.State.Eval.Options {
		if oe.OptionID == "OPT-01" && (oe.Status != OptApproved || oe.ValidationWeeks != 0 || oe.StartMonth != "2027-04") {
			t.Errorf("OPT-01 모의 결정: %s %d주 산입 %s", oe.Status, oe.ValidationWeeks, oe.StartMonth)
		}
	}

	// 잘못된 값·모르는 필드는 거절
	for _, bad := range []string{`{"price_cut_pct":5}`, `{"fx_shift":-5000}`, `{"options":{"OPT-03":"승인"}}`, `{"delivery_start":"2020-01"}`, `{"unknown":1}`, `{}`} {
		if _, err := svc.SubmitIn(ctx, fv.Session.ID, EvSimAssumption, "시뮬레이터", []byte(bad)); !errors.Is(err, ErrInvalid) {
			t.Errorf("잘못된 모의 가정이 통과: %s (%v)", bad, err)
		}
	}
}

func raws(vs ...any) []json.RawMessage {
	out := make([]json.RawMessage, len(vs))
	for i, v := range vs {
		out[i], _ = json.Marshal(v)
	}
	return out
}

func TestSweepMatchesSingleEvaluation(t *testing.T) {
	b := loadBase(t)
	sc := DefaultScenario()
	x := SweepAxis{Param: "price_cut_pct", Values: raws(0, -5, -10)}
	y := &SweepAxis{Param: "delivery_start", Values: raws("2026-10", "2027-01")}
	res, err := Sweep(b, sc, x, y)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Cells) != 2 || len(res.Cells[0]) != 3 || strings.Join(res.XValues, ",") != "0,-5,-10" || strings.Join(res.YValues, ",") != "2026-10,2027-01" {
		t.Fatalf("격자 모양: %v × %v, %d행", res.XValues, res.YValues, len(res.Cells))
	}
	for yi, ds := range []string{"2026-10", "2027-01"} {
		for xi, cut := range []float64{0, -0.05, -0.10} {
			c := DefaultScenario()
			c.PriceCut, c.DeliveryStart = cut, ds
			want := sweepMetrics(evaluate(b, c))
			for k, w := range want {
				if g := res.Cells[yi][xi][k]; math.Abs(g-w) > 1e-6 {
					t.Errorf("칸(%s, %v) %s = %v, 단일 평가 %v", ds, cut, k, g, w)
				}
			}
		}
	}
	// 기본 시나리오 자체는 바뀌지 않는다
	if base := sweepMetrics(evaluate(b, sc)); math.Abs(base["pl02_coverage_pct"]-res.Base["pl02_coverage_pct"]) > 1e-9 || sc.PriceCut != 0 {
		t.Error("격자가 원래 시나리오를 바꿨다")
	}
	// 개시 2027-01이면 OPT-01 합류가 계약 안에 더 많이 들어와 공급률이 높다
	if !(res.Cells[1][0]["pl02_coverage_pct"] > res.Cells[0][0]["pl02_coverage_pct"]) {
		t.Errorf("개시 2027-01 공급률 %.1f가 2026-10 %.1f보다 높아야 한다", res.Cells[1][0]["pl02_coverage_pct"], res.Cells[0][0]["pl02_coverage_pct"])
	}

	if _, err := Sweep(b, sc, SweepAxis{Param: "nope", Values: raws(1)}, nil); err == nil {
		t.Error("모르는 변수가 통과")
	}
	many := make([]any, 25)
	for i := range many {
		many[i] = -float64(i) / 5
	}
	if _, err := Sweep(b, sc, SweepAxis{Param: "price_cut_pct", Values: raws(many...)}, &SweepAxis{Param: "fx_shift", Values: raws(many...)}); err == nil {
		t.Error("400칸 초과가 통과")
	}
}
