package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// checkAccess는 계정·권한 표(이슈 #55)를 저장소마다 같은 기대로 본다: 비어 있을 때만 넣기, 역할·권한 읽기,
// 계정 찾기, 로그인 세션 만료·삭제.
func checkAccess(t *testing.T, st Store) {
	t.Helper()
	ctx := context.Background()
	roles := []Role{
		{Code: "sales", DeptID: "영업", Dept: "영업", Label: "영업", Duty: "RFP 접수", Sort: 1},
		{Code: "board", DeptID: "CEO/이사회", Dept: "경영진", Label: "CEO/이사회", Sort: 2},
	}
	perms := []Perm{{"sales", PermEvent, "E1_RFP"}, {"sales", PermFeature, "view"}, {"board", PermEvent, "E2_DECISION"}}
	users := []User{{LoginID: "sales01", Name: "김영업", RoleCode: "sales", PasswordHash: "h1"}, {LoginID: "ceo01", Name: "오대표", RoleCode: "board", PasswordHash: "h2"}}
	seeded, err := st.SeedAccess(ctx, roles, perms, users)
	if err != nil || !seeded {
		t.Fatalf("처음 넣기: %v %v", seeded, err)
	}
	// 두 번째는 아무것도 바꾸지 않는다(이미 있는 표는 DB가 기준)
	if seeded, err := st.SeedAccess(ctx, roles[:1], perms[:1], users[:1]); err != nil || seeded {
		t.Fatalf("다시 넣기: %v %v", seeded, err)
	}
	rs, ps, err := st.ListRoles(ctx)
	if err != nil || len(rs) != 2 || rs[0].Code != "sales" || rs[1].DeptID != "CEO/이사회" || len(ps) != 3 {
		t.Fatalf("역할·권한: %+v %+v %v", rs, ps, err)
	}
	us, err := st.ListUsers(ctx)
	if err != nil || len(us) != 2 {
		t.Fatalf("계정 목록: %+v %v", us, err)
	}
	u, err := st.GetUser(ctx, "ceo01")
	if err != nil || u.Name != "오대표" || u.RoleCode != "board" || u.PasswordHash != "h2" {
		t.Fatalf("계정 찾기: %+v %v", u, err)
	}
	if _, err := st.GetUser(ctx, "없음"); !errors.Is(err, ErrNoUser) {
		t.Fatalf("없는 계정: %v", err)
	}
	now := time.Now()
	if err := st.CreateLogin(ctx, "tok-a", "sales01", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateLogin(ctx, "tok-old", "ceo01", now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if u, err := st.LoginUser(ctx, "tok-a", now); err != nil || u.LoginID != "sales01" {
		t.Fatalf("로그인 세션: %+v %v", u, err)
	}
	if _, err := st.LoginUser(ctx, "tok-old", now); !errors.Is(err, ErrNoLogin) {
		t.Fatalf("만료된 세션: %v", err)
	}
	if _, err := st.LoginUser(ctx, "tok-a", now.Add(2*time.Hour)); !errors.Is(err, ErrNoLogin) {
		t.Fatalf("시간이 지나 만료: %v", err)
	}
	if err := st.DeleteLogin(ctx, "tok-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.LoginUser(ctx, "tok-a", now); !errors.Is(err, ErrNoLogin) {
		t.Fatalf("지운 세션: %v", err)
	}
}

func TestMemoryAccess(t *testing.T) { checkAccess(t, NewMemory()) }

func TestPostgresAccess(t *testing.T) {
	url := pgURL(t)
	ctx := context.Background()
	st, err := Open(ctx, Config{DatabaseURL: url, Wait: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	// 테스트 DB는 매번 새것이지만, 다른 테스트가 먼저 넣었을 수 있으니 비우고 시작한다
	if _, err := st.(*postgres).pool.Exec(ctx, "TRUNCATE app_logins, app_users, app_role_permissions, app_roles"); err != nil {
		t.Fatal(err)
	}
	checkAccess(t, st)
}
