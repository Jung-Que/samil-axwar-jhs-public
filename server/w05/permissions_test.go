package w05

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"axwar/server/store"
)

// 이슈 #43: 부서별 권한.

func TestDepartmentTable(t *testing.T) {
	codes, ids := map[string]bool{}, map[string]bool{}
	for _, d := range Departments {
		if codes[d.Code] || ids[d.ID] {
			t.Errorf("중복 코드·ID: %s %s", d.Code, d.ID)
		}
		codes[d.Code], ids[d.ID] = true, true
		for _, f := range commonFeatures {
			if !HasFeature(d.ID, f) {
				t.Errorf("%s에 공통 기능 %s 없음", d.ID, f)
			}
		}
	}
	if d, ok := DeptByCode("sales-exec"); !ok || d.ID != "영업 임원" || d.Dept != "영업" {
		t.Errorf("영업 임원은 영업 부서 안의 권한: %+v", d)
	}
	if HasFeature("영업", FeatMainReset) || !HasFeature("관리자", FeatMainReset) || !HasFeature("관리자", FeatSessionDrop) {
		t.Error("기준 세션 초기화·세션 삭제는 운영 관리만")
	}
}

func TestEventPermissionsByDepartment(t *testing.T) {
	ctx := context.Background()
	b := loadBase(t)
	svc, _ := NewService(ctx, b, store.NewMemory())
	rfp, _ := json.Marshal(RFPFromSource())
	// 사건 권한표: 생산관리는 RFP를 접수하지 못한다
	if _, err := svc.Submit(ctx, EvRFP, "생산관리", rfp); !errors.Is(err, ErrInvalid) {
		t.Fatalf("생산관리 RFP 접수가 거절되지 않았다: %v", err)
	}
	if _, err := svc.SubmitAs(ctx, store.MainSession, "영업", EvRFP, "", rfp); err != nil {
		t.Fatal(err)
	}
	// 회신은 요청 대상 부서만: 영업이 CR-01(생산관리)에 회신 못 함
	if _, err := svc.SubmitAs(ctx, store.MainSession, "영업", EvReply, "", []byte(`{"request_id":"CR-01","answer":{"capacity_period":"annual"}}`)); !errors.Is(err, ErrInvalid) {
		t.Errorf("영업의 CR-01 회신이 거절되지 않았다: %v", err)
	}
	v, err := svc.SubmitAs(ctx, store.MainSession, "생산관리", EvReply, "", []byte(`{"request_id":"CR-01","answer":{"capacity_period":"annual"}}`))
	if err != nil {
		t.Fatalf("생산관리 CR-01 회신 거절: %v", err)
	}
	last := v.State.Log[len(v.State.Log)-1].Event
	if last.Actor != "생산관리" || last.By != "생산관리" {
		t.Errorf("기준 세션 입력 기록: actor %q by %q", last.Actor, last.By)
	}
	// 기준 세션: 다른 부서 권한으로 입력하면 403
	if _, err := svc.SubmitAs(ctx, store.MainSession, "영업", EvForApprove, "CEO/이사회", []byte(`{}`)); !errors.Is(err, ErrForbidden) {
		t.Errorf("기준 세션 대행이 403이 아니다: %v", err)
	}
	// 부서 없는 사용자
	if _, err := svc.SubmitAs(ctx, store.MainSession, "", EvForApprove, "", []byte(`{}`)); !errors.Is(err, ErrNoRole) {
		t.Errorf("부서 없는 입력: %v", err)
	}
	// 기준 세션 초기화·세션 삭제는 운영 관리만
	if _, err := svc.ResetAs(ctx, store.MainSession, "재무·전략", ""); !errors.Is(err, ErrForbidden) {
		t.Errorf("재무·전략의 기준 세션 초기화: %v", err)
	}

	// 시뮬레이션 세션: 재무·전략이 영업·CEO/이사회 권한을 대행해 흐름을 굴리고, 입력 부서가 남는다
	fv, err := svc.ForkAs(ctx, "재무·전략", store.MainSession, len(v.State.Log), "재무 시뮬레이션")
	if err != nil {
		t.Fatal(err)
	}
	sid := fv.Session.ID
	if fv, err = svc.SubmitAs(ctx, sid, "재무·전략", EvForApprove, "영업", []byte(`{}`)); err != nil {
		t.Fatalf("시뮬레이션 대행 상신: %v", err)
	}
	if fv, err = svc.SubmitAs(ctx, sid, "재무·전략", EvDecision, "CEO/이사회", []byte(`{"participation":"조건부","options":{"OPT-02":"승인","OPT-01":"보류"}}`)); err != nil {
		t.Fatalf("시뮬레이션 대행 결정: %v", err)
	}
	d := fv.State.Decisions[len(fv.State.Decisions)-1]
	if d.Approver != "CEO/이사회" || d.EnteredBy != "재무·전략" {
		t.Errorf("대행 결정 기록: approver %q entered_by %q", d.Approver, d.EnteredBy)
	}
	// 대행이어도 권한표는 지킨다: 시계(E0)는 영업 권한으로 못 넣는다(시스템·운영 관리)
	if _, err := svc.SubmitAs(ctx, sid, "재무·전략", EvClock, "영업", []byte(`{"sim_date":"2026-07-10"}`)); !errors.Is(err, ErrInvalid) {
		t.Errorf("권한표 밖 대행(영업 권한 시계)이 통과: %v", err)
	}
	// 모의 가정은 사용자 부서 이름으로 남는다
	if fv, err = svc.SubmitAs(ctx, sid, "재무·전략", EvSimAssumption, "", []byte(`{"fx_shift":-50}`)); err != nil {
		t.Fatalf("재무·전략 모의 가정: %v", err)
	}
	if e := fv.State.Log[len(fv.State.Log)-1].Event; e.Actor != "재무·전략" {
		t.Errorf("모의 가정 행위자 %q", e.Actor)
	}
	if err := svc.DeleteSessionAs(ctx, "재무·전략", sid); !errors.Is(err, ErrForbidden) {
		t.Errorf("재무·전략의 세션 삭제: %v", err)
	}
	if err := svc.DeleteSessionAs(ctx, "관리자", sid); err != nil {
		t.Errorf("운영 관리의 세션 삭제: %v", err)
	}
}
