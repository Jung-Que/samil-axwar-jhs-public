package w05

import (
	"context"
	"errors"
	"strings"
	"testing"

	"axwar/server/store"
)

// 이슈 #37: 세션·분기·모의 제출·비교.

func TestForkIsIndependent(t *testing.T) {
	ctx := context.Background()
	b := loadBase(t)
	svc, err := NewService(ctx, b, store.NewMemory())
	if err != nil {
		t.Fatal(err)
	}
	main, err := svc.RunScenario(ctx, "normal", "")
	if err != nil {
		t.Fatal(err)
	}
	if main.Session.ID != store.MainSession || main.State.SessionKind != store.KindMain {
		t.Fatalf("기준 세션 정보: %+v / %s", main.Session, main.State.SessionKind)
	}
	// 기준 세션 제출은 [모의]가 아니다
	if strings.Contains(strings.Join(main.State.Log[len(main.State.Log)-1].Changes, " "), "[모의]") {
		t.Error("기준 세션 제출에 [모의] 표시가 붙었다")
	}

	// 결정(사건 3) 직후에서 분기 → 바이어 -7% 회신 → 결정 낡음·재검토. 기준 세션은 그대로
	fv, err := svc.Fork(ctx, store.MainSession, 3, "")
	if err != nil {
		t.Fatal(err)
	}
	fid := fv.Session.ID
	if fv.Session.Kind != store.KindSim || fv.Session.ParentID != store.MainSession || fv.Session.ForkSeq != 3 || len(fv.State.Log) != 3 {
		t.Fatalf("분기 세션: %+v, 사건 %d", fv.Session, len(fv.State.Log))
	}
	wantS(t, "분기 직후 입찰", fv.State.Bid.State, BidConditional)
	fv, err = svc.SubmitIn(ctx, fid, EvBuyer, "영업", []byte(`{"price_cut_pct":-7}`))
	if err != nil {
		t.Fatal(err)
	}
	wantS(t, "분기 세션 입찰", fv.State.Bid.State, BidRereview)
	mv, _ := svc.Snapshot(ctx)
	wantS(t, "기준 세션 입찰(그대로)", mv.State.Bid.State, BidAwaiting)
	wantInt(t, "기준 세션 사건 수", int64(len(mv.State.Log)), 4)

	// 비교: 입찰 상태·인하율이 다르다
	c, err := svc.Compare(ctx, store.MainSession, fid)
	if err != nil {
		t.Fatal(err)
	}
	if c.A.BidState != BidAwaiting || c.B.BidState != BidRereview || c.B.PriceCut != -0.07 || c.A.PriceCut != 0 {
		t.Errorf("비교 KPI: A %+v / B %+v", c.A, c.B)
	}
	if len(c.Products[ProductDerma]) != 12 {
		t.Errorf("PL-02 월 비교 %d개월, 기대 12", len(c.Products[ProductDerma]))
	}
	if len(c.Tasks) == 0 {
		t.Error("작업 차이가 없다(분기 세션에 재상신 작업이 있어야 함)")
	}

	// 분기 세션에서 재상신·재승인·제출 → [모의] 제출, 가예약(모의)
	for _, st := range []struct{ typ, actor, payload string }{
		{EvForApprove, "영업", `{}`},
		{EvDecision, "CEO/이사회", `{"participation":"조건부","options":{"OPT-02":"승인","OPT-01":"보류"},"conditions":["COND_PL01_UNCONFIRMED"],"price_floor_pct":-10}`},
		{EvSubmit, "영업", `{}`},
	} {
		if fv, err = svc.SubmitIn(ctx, fid, st.typ, st.actor, []byte(st.payload)); err != nil {
			t.Fatalf("분기 세션 %s: %v", st.typ, err)
		}
	}
	wantS(t, "분기 세션 제출 뒤", fv.State.Bid.State, BidAwaiting)
	if !strings.Contains(strings.Join(fv.State.Log[len(fv.State.Log)-1].Changes, " "), "[모의]") || fv.State.Reservations[0].State != "가예약(모의)" {
		t.Errorf("시뮬레이션 세션 제출이 [모의]로 표시되지 않았다: %v / %s", fv.State.Log[len(fv.State.Log)-1].Changes, fv.State.Reservations[0].State)
	}

	// 범위 밖 분기, 없는 세션, main 삭제
	if _, err := svc.Fork(ctx, fid, 99, ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("범위 밖 분기: %v", err)
	}
	if _, err := svc.SnapshotIn(ctx, "sim-none"); !errors.Is(err, ErrNotFound) {
		t.Errorf("없는 세션: %v", err)
	}
	if err := svc.DeleteSession(ctx, store.MainSession); !errors.Is(err, ErrInvalid) {
		t.Errorf("main 삭제: %v", err)
	}
	if err := svc.DeleteSession(ctx, fid); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SnapshotIn(ctx, fid); !errors.Is(err, ErrNotFound) {
		t.Errorf("지운 세션: %v", err)
	}
}

func TestStartScenarioKeepsSessions(t *testing.T) {
	ctx := context.Background()
	b := loadBase(t)
	svc, _ := NewService(ctx, b, store.NewMemory())
	v1, err := svc.StartScenario(ctx, "normal", "")
	if err != nil {
		t.Fatal(err)
	}
	v2, err := svc.StartScenario(ctx, "reject", "2025-12-31")
	if err != nil {
		t.Fatal(err)
	}
	if v1.Session.ID == v2.Session.ID || v2.State.Scenario.AsOf != "2025-12-31" || !strings.Contains(v2.Session.Name, "정보 부족·거절 시연 · 2025-12-31") {
		t.Fatalf("시연 세션: %+v / %+v", v1.Session, v2.Session)
	}
	again, _ := svc.SnapshotIn(ctx, v1.Session.ID)
	wantS(t, "첫 시연 세션(그대로)", again.State.Bid.State, BidAwaiting)
	if len(v2.Sessions) != 3 {
		t.Errorf("세션 목록 %d개, 기대 3(main + 시연 2)", len(v2.Sessions))
	}
}
