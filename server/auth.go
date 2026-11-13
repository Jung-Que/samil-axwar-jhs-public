package main

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"axwar/server/store"
	"axwar/server/w05"
)

// 로그인(이슈 #55). 시연용이라 계정은 부서마다 하나씩 처음 뜰 때 넣고, 회원 가입·비밀번호 변경·외부 인증은 없다.
// 계정·권한은 DB 표(app_users·app_roles·app_role_permissions, store/access.go)에 있고, 서버는 시작할 때 역할·권한 표를
// 읽어 권한 판단(w05.Departments)에 쓴다. 사용자 부서는 로그인한 계정의 역할이다.
//
//	POST /api/auth/login     {login_id, password} → 로그인 쿠키(HttpOnly·SameSite=Lax), 계정 정보
//	POST /api/auth/logout    로그인 세션 지우기
//	GET  /api/auth/me        지금 로그인한 계정(없으면 401)
//	GET  /api/auth/accounts  시연 계정 목록(로그인 화면용, 비밀번호 해시는 내보내지 않는다)
//	GET  /api/auth/users     계정·역할·권한 표(로그인 필요)

const (
	loginCookie = "w05_login"
	loginTTL    = 12 * time.Hour
	pbkdf2Iter  = 120_000
)

// 기본 시연 비밀번호. 배포에서 바꾸려면 처음 뜨기 전에 W05_DEMO_PASSWORD를 넣는다(계정 표가 비어 있을 때만 쓰인다).
const defaultDemoPassword = "w05demo"

func demoPassword() string {
	if v := os.Getenv("W05_DEMO_PASSWORD"); v != "" {
		return v
	}
	return defaultDemoPassword
}

// demoAccount는 처음 뜰 때 넣는 시연 계정이다(부서마다 하나, 이름은 가상).
type demoAccount struct{ LoginID, Name, Role string }

var demoAccounts = []demoAccount{
	{"sales01", "김영업", "sales"},
	{"salesexec01", "박임원", "sales-exec"},
	{"prod01", "이생산", "production"},
	{"qa01", "최품질", "purchasing"},
	{"masil01", "정마실", "masil-scm"},
	{"fin01", "한재무", "finance"},
	{"ceo01", "오대표", "board"},
	{"admin01", "윤관리", "admin"},
}

// ---- 비밀번호 해시: pbkdf2-sha256$반복$솔트$해시(base64) ----

func hashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, pw, salt, pbkdf2Iter, 32)
	if err != nil {
		return "", err
	}
	enc := base64.RawStdEncoding
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", pbkdf2Iter, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// dummyHash는 없는 아이디로 로그인할 때 비교에 쓰는 해시다(응답 시간으로 아이디 유무가 드러나지 않게).
var dummyHash = sync.OnceValue(func() string {
	h, _ := hashPassword("없는 계정")
	return h
})

func checkPassword(hash, pw string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err1 := enc.DecodeString(parts[2])
	want, err2 := enc.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, iter, len(want))
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}

// ---- 역할·권한 표 ↔ 권한표(w05.Department) ----

func rolesFromDepartments(ds []w05.Department) ([]store.Role, []store.Perm) {
	var roles []store.Role
	var perms []store.Perm
	for i, d := range ds {
		roles = append(roles, store.Role{Code: d.Code, DeptID: d.ID, Dept: d.Dept, Label: d.Label, Duty: d.Duty, Sort: i + 1})
		add := func(kind string, items []string) {
			for _, it := range items {
				perms = append(perms, store.Perm{RoleCode: d.Code, Kind: kind, Item: it})
			}
		}
		add(store.PermEvent, d.Events)
		add(store.PermReply, d.Replies)
		add(store.PermFeature, d.Features)
	}
	return roles, perms
}

func departmentsFromRoles(roles []store.Role, perms []store.Perm) []w05.Department {
	ds := make([]w05.Department, 0, len(roles))
	for _, r := range roles {
		d := w05.Department{Code: r.Code, ID: r.DeptID, Dept: r.Dept, Label: r.Label, Duty: r.Duty}
		for _, p := range perms {
			if p.RoleCode != r.Code {
				continue
			}
			switch p.Kind {
			case store.PermEvent:
				d.Events = append(d.Events, p.Item)
			case store.PermReply:
				d.Replies = append(d.Replies, p.Item)
			case store.PermFeature:
				d.Features = append(d.Features, p.Item)
			}
		}
		ds = append(ds, d)
	}
	return ds
}

// setupAccess는 비어 있는 계정·권한 표에 기본값을 넣고, 역할·권한 표를 읽어 권한표로 쓴다.
// 표에 이미 있는 값은 덮어쓰지 않는다(DB가 기준).
func setupAccess(ctx context.Context, st store.Store) error {
	roles, perms := rolesFromDepartments(w05.Departments)
	pw := demoPassword()
	var users []store.User
	for _, a := range demoAccounts {
		h, err := hashPassword(pw)
		if err != nil {
			return err
		}
		users = append(users, store.User{LoginID: a.LoginID, Name: a.Name, RoleCode: a.Role, PasswordHash: h})
	}
	seeded, err := st.SeedAccess(ctx, roles, perms, users)
	if err != nil {
		return fmt.Errorf("계정·권한 표 준비 실패: %w", err)
	}
	if seeded {
		log.Printf("auth: 비어 있던 계정·권한 표에 기본 권한표와 시연 계정 %d개를 넣었다", len(users))
	}
	rs, ps, err := st.ListRoles(ctx)
	if err != nil {
		return fmt.Errorf("역할·권한 표 읽기 실패: %w", err)
	}
	if len(rs) == 0 {
		return errors.New("역할 표가 비어 있다")
	}
	w05.SetDepartments(departmentsFromRoles(rs, ps))
	return nil
}

// ---- 로그인 세션 ----

func tokenHash(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

type ctxUser struct{}

// userOf는 요청의 로그인 쿠키로 계정을 찾는다.
func userOf(r *http.Request, st store.Store) (store.User, bool) {
	c, err := r.Cookie(loginCookie)
	if err != nil || c.Value == "" {
		return store.User{}, false
	}
	u, err := st.LoginUser(r.Context(), tokenHash(c.Value), time.Now())
	if err != nil {
		if !errors.Is(err, store.ErrNoLogin) {
			log.Printf("auth: 로그인 확인 실패: %v", err)
		}
		return store.User{}, false
	}
	return u, true
}

// requireLogin은 로그인한 요청만 통과시키고 계정을 요청 문맥에 싣는다(없으면 401).
func requireLogin(st store.Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := userOf(r, st)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "로그인이 필요합니다."})
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxUser{}, u)))
	})
}

// roleOf는 로그인한 계정의 부서 ID다(권한 판단에 쓴다). 로그인하지 않았으면 "" → 서비스가 401.
func roleOf(r *http.Request) string {
	u, ok := r.Context().Value(ctxUser{}).(store.User)
	if !ok {
		return ""
	}
	if d, ok := w05.DeptByCode(u.RoleCode); ok {
		return d.ID
	}
	return ""
}

// secure는 HTTPS로 들어온 요청인지 본다(배포는 앞단 라우터가 X-Forwarded-Proto를 붙인다).
func secure(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

type userOut struct {
	LoginID   string `json:"login_id"`
	Name      string `json:"name"`
	RoleCode  string `json:"role_code"`
	DeptID    string `json:"dept_id"`
	DeptLabel string `json:"dept_label"`
	CreatedAt string `json:"created_at,omitempty"`
}

func outOf(u store.User) userOut {
	o := userOut{LoginID: u.LoginID, Name: u.Name, RoleCode: u.RoleCode}
	if d, ok := w05.DeptByCode(u.RoleCode); ok {
		o.DeptID, o.DeptLabel = d.ID, d.Label
	}
	if !u.CreatedAt.IsZero() {
		o.CreatedAt = u.CreatedAt.UTC().Format(time.RFC3339)
	}
	return o
}

func registerAuth(mux *http.ServeMux, st store.Store) {
	mux.HandleFunc("POST /api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			LoginID  string `json:"login_id"`
			Password string `json:"password"`
		}
		if !decode(w, r, &in) {
			return
		}
		u, err := st.GetUser(r.Context(), strings.TrimSpace(in.LoginID))
		if err != nil && !errors.Is(err, store.ErrNoUser) {
			writeErr(w, err)
			return
		}
		// 없는 아이디도 같은 시간이 걸리게 해시를 한 번 계산한다
		hash := u.PasswordHash
		if hash == "" {
			hash = dummyHash()
		}
		if !checkPassword(hash, in.Password) || u.LoginID == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "아이디 또는 비밀번호가 올바르지 않습니다. 다시 입력해 주세요."})
			return
		}
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			writeErr(w, err)
			return
		}
		tok := base64.RawURLEncoding.EncodeToString(raw)
		exp := time.Now().Add(loginTTL)
		if err := st.CreateLogin(r.Context(), tokenHash(tok), u.LoginID, exp); err != nil {
			writeErr(w, err)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: loginCookie, Value: tok, Path: "/", Expires: exp, MaxAge: int(loginTTL.Seconds()),
			HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: secure(r)})
		writeJSON(w, http.StatusOK, map[string]any{"user": outOf(u)})
	})

	mux.HandleFunc("POST /api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(loginCookie); err == nil && c.Value != "" {
			if err := st.DeleteLogin(r.Context(), tokenHash(c.Value)); err != nil {
				log.Printf("auth: 로그아웃 기록 실패: %v", err)
			}
		}
		http.SetCookie(w, &http.Cookie{Name: loginCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: secure(r)})
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	mux.HandleFunc("GET /api/auth/me", func(w http.ResponseWriter, r *http.Request) {
		u, ok := userOf(r, st)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "로그인이 필요합니다."})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"user": outOf(u)})
	})

	// 시연 계정 목록: 로그인 화면에서 고르게 한다. 기본 시연 비밀번호를 쓰는 동안만 그 값을 알려 준다.
	mux.HandleFunc("GET /api/auth/accounts", func(w http.ResponseWriter, r *http.Request) {
		us, err := st.ListUsers(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		out := make([]map[string]string, 0, len(us))
		for _, u := range us {
			o := outOf(u)
			duty := ""
			if d, ok := w05.DeptByCode(u.RoleCode); ok {
				duty = d.Duty
			}
			out = append(out, map[string]string{"login_id": o.LoginID, "name": o.Name, "role_code": o.RoleCode, "dept_label": o.DeptLabel, "duty": duty})
		}
		body := map[string]any{"accounts": out}
		if os.Getenv("W05_DEMO_PASSWORD") == "" {
			body["demo_password"] = defaultDemoPassword
		}
		writeJSON(w, http.StatusOK, body)
	})

	mux.Handle("GET /api/auth/users", requireLogin(st, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		us, err := st.ListUsers(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		rs, ps, err := st.ListRoles(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		users := make([]userOut, 0, len(us))
		for _, u := range us {
			users = append(users, outOf(u))
		}
		roles := make([]map[string]any, 0, len(rs))
		for _, rr := range rs {
			n := 0
			for _, u := range us {
				if u.RoleCode == rr.Code {
					n++
				}
			}
			roles = append(roles, map[string]any{"code": rr.Code, "dept_id": rr.DeptID, "dept": rr.Dept, "label": rr.Label, "duty": rr.Duty, "users": n})
		}
		permRows := make([]map[string]string, 0, len(ps))
		for _, p := range ps {
			permRows = append(permRows, map[string]string{"role_code": p.RoleCode, "kind": p.Kind, "item": p.Item})
		}
		writeJSON(w, http.StatusOK, map[string]any{"users": users, "roles": roles, "permissions": permRows, "store": st.Kind()})
	})))
}
