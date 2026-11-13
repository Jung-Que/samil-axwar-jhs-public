package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"axwar/server/store"
	"axwar/server/w05"
)

// 로그인(이슈 #55): 틀린 비밀번호 401, 로그인 쿠키(HttpOnly), me·계정 표, 로그아웃 뒤 401.
func TestAuthAPI(t *testing.T) {
	st := store.NewMemory()
	h := newHandler(fakeBuild(t), st, newTestService(t, st))
	call := func(method, path, body string, c *http.Cookie) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if c != nil {
			req.AddCookie(c)
		}
		h.ServeHTTP(rec, req)
		return rec
	}
	if rec := call("POST", "/api/auth/login", `{"login_id":"prod01","password":"틀림"}`, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("틀린 비밀번호: %d", rec.Code)
	}
	if rec := call("POST", "/api/auth/login", `{"login_id":"없음","password":"w05demo"}`, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("없는 아이디: %d", rec.Code)
	}
	rec := call("POST", "/api/auth/login", `{"login_id":"prod01","password":"w05demo"}`, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"dept_id":"생산관리"`) {
		t.Fatalf("로그인: %d %s", rec.Code, rec.Body.String())
	}
	var c *http.Cookie
	for _, x := range rec.Result().Cookies() {
		if x.Name == loginCookie {
			c = x
		}
	}
	if c == nil || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
		t.Fatalf("로그인 쿠키: %+v", c)
	}
	if rec := call("GET", "/api/auth/me", "", c); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"name":"이생산"`) {
		t.Fatalf("me: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call("GET", "/api/auth/users", "", c); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"login_id":"ceo01"`) || strings.Contains(rec.Body.String(), "pbkdf2") {
		t.Fatalf("계정 표(해시는 내보내지 않음): %d %.300s", rec.Code, rec.Body.String())
	}
	if rec := call("GET", "/api/auth/accounts", "", nil); strings.Contains(rec.Body.String(), "pbkdf2") || !strings.Contains(rec.Body.String(), `"demo_password":"w05demo"`) {
		t.Fatalf("시연 계정 목록: %.300s", rec.Body.String())
	}
	// 생산관리는 기준 세션에서 영업 사건(E1)을 넣을 수 없다(권한표 위반은 409)
	if rec := call("POST", "/api/w05/events", `{"type":"E1_RFP","payload":{}}`, c); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "E1_RFP는 영업 권한") {
		t.Fatalf("생산관리의 E1: %d %.200s", rec.Code, rec.Body.String())
	}
	if rec := call("POST", "/api/auth/logout", "", c); rec.Code != 200 {
		t.Fatalf("로그아웃: %d", rec.Code)
	}
	if rec := call("GET", "/api/w05/state", "", c); rec.Code != http.StatusUnauthorized {
		t.Fatalf("로그아웃한 쿠키로 조회: %d", rec.Code)
	}
}

// 권한 판단은 DB 역할·권한 표에서 온다: 표에서 E1(RFP 접수) 권한을 영업에서 생산관리로 옮기면 영업은 넣지 못한다.
func TestPermissionsFromTables(t *testing.T) {
	orig := append([]w05.Department(nil), w05.Departments...)
	t.Cleanup(func() { w05.SetDepartments(orig) })
	st := store.NewMemory()
	roles, perms := rolesFromDepartments(w05.Departments)
	perms = slices.DeleteFunc(perms, func(p store.Perm) bool { return p.RoleCode == "sales" && p.Item == w05.EvRFP })
	perms = append(perms, store.Perm{RoleCode: "production", Kind: store.PermEvent, Item: w05.EvRFP})
	if _, err := st.SeedAccess(context.Background(), roles, perms, nil); err != nil {
		t.Fatal(err)
	}
	h := newHandler(fakeBuild(t), st, newTestService(t, st)) // setupAccess: 역할 표가 이미 있으니 그대로 읽는다
	if rec := doAs(h, "sales", "POST", "/api/w05/events", `{"type":"E1_RFP","payload":{}}`); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "E1_RFP는 생산관리 권한") {
		t.Fatalf("표에서 옮긴 권한: %d %.200s", rec.Code, rec.Body.String())
	}
	w05.SetDepartments(orig)
	if rec := doAs(h, "sales", "POST", "/api/w05/events", `{"type":"E1_RFP","payload":{}}`); strings.Contains(rec.Body.String(), "권한 없음") {
		t.Fatalf("기본 권한표로 되돌린 뒤에도 막힘: %d %.200s", rec.Code, rec.Body.String())
	}
}

func TestPasswordHash(t *testing.T) {
	h, err := hashPassword("비밀")
	if err != nil || !strings.HasPrefix(h, "pbkdf2-sha256$120000$") {
		t.Fatalf("해시 %q %v", h, err)
	}
	if !checkPassword(h, "비밀") || checkPassword(h, "비밀2") || checkPassword("엉터리", "비밀") {
		t.Fatal("비밀번호 확인이 틀림")
	}
	if h2, _ := hashPassword("비밀"); h == h2 {
		t.Fatal("솔트가 같다")
	}
}
