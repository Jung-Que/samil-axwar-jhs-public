package store

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// 계정·권한 표(이슈 #55). 시연용 로그인이라 회원 가입·비밀번호 변경·외부 인증은 없다.
// 역할(부서)과 역할별 권한, 계정, 로그인 세션을 저장한다. 서버는 시작할 때 역할·권한을 읽어 권한 판단에 쓴다.

// Role은 권한 표의 역할(부서) 한 줄이다. Code는 영문 코드, DeptID는 사건 actor_role에 쓰는 이름이다.
type Role struct {
	Code   string
	DeptID string
	Dept   string
	Label  string
	Duty   string
	Sort   int
}

// 권한 종류: event = 기준 세션에서 넣는 사건, reply = 회신하는 확인 요청, feature = 화면 기능.
const (
	PermEvent   = "event"
	PermReply   = "reply"
	PermFeature = "feature"
)

// Perm은 역할이 가진 권한 하나다.
type Perm struct {
	RoleCode string
	Kind     string
	Item     string
}

// User는 로그인 계정이다. PasswordHash는 해시 문자열이고 평문 비밀번호는 저장하지 않는다.
type User struct {
	LoginID      string
	Name         string
	RoleCode     string
	PasswordHash string
	CreatedAt    time.Time
}

var (
	ErrNoUser  = errors.New("계정이 없다")
	ErrNoLogin = errors.New("로그인 세션이 없거나 만료됐다")
)

// ---- 메모리 저장소 ----

type memAccess struct {
	roles  []Role
	perms  []Perm
	users  []User
	logins map[string]memLogin
}

type memLogin struct {
	loginID string
	expires time.Time
}

func (m *memory) SeedAccess(_ context.Context, roles []Role, perms []Perm, users []User) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	seeded := false
	if len(m.acc.roles) == 0 {
		m.acc.roles = append([]Role(nil), roles...)
		m.acc.perms = append([]Perm(nil), perms...)
		seeded = true
	}
	if len(m.acc.users) == 0 {
		for _, u := range users {
			if !slices.ContainsFunc(m.acc.roles, func(r Role) bool { return r.Code == u.RoleCode }) {
				return false, errors.New("계정의 역할이 역할 표에 없다: " + u.RoleCode)
			}
			if u.CreatedAt.IsZero() {
				u.CreatedAt = time.Now().UTC()
			}
			m.acc.users = append(m.acc.users, u)
		}
		seeded = seeded || len(users) > 0
	}
	return seeded, nil
}

func (m *memory) ListRoles(context.Context) ([]Role, []Perm, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rs := append([]Role(nil), m.acc.roles...)
	slices.SortStableFunc(rs, func(a, b Role) int { return a.Sort - b.Sort })
	return rs, append([]Perm(nil), m.acc.perms...), nil
}

func (m *memory) ListUsers(context.Context) ([]User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]User(nil), m.acc.users...), nil
}

func (m *memory) GetUser(_ context.Context, loginID string) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.acc.users {
		if u.LoginID == loginID {
			return u, nil
		}
	}
	return User{}, ErrNoUser
}

func (m *memory) CreateLogin(_ context.Context, tokenHash, loginID string, expires time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !slices.ContainsFunc(m.acc.users, func(u User) bool { return u.LoginID == loginID }) {
		return ErrNoUser
	}
	now := time.Now()
	for k, l := range m.acc.logins { // 만료된 로그인은 이때 정리한다
		if !l.expires.After(now) {
			delete(m.acc.logins, k)
		}
	}
	m.acc.logins[tokenHash] = memLogin{loginID: loginID, expires: expires}
	return nil
}

func (m *memory) LoginUser(_ context.Context, tokenHash string, now time.Time) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.acc.logins[tokenHash]
	if !ok || !l.expires.After(now) {
		return User{}, ErrNoLogin
	}
	for _, u := range m.acc.users {
		if u.LoginID == l.loginID {
			return u, nil
		}
	}
	return User{}, ErrNoLogin
}

func (m *memory) DeleteLogin(_ context.Context, tokenHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.acc.logins, tokenHash)
	return nil
}

// ---- Postgres ----

// SeedAccess는 역할 표가 비어 있으면 역할·권한을, 계정 표가 비어 있으면 계정을 한 트랜잭션에서 넣는다.
// 이미 있는 표는 건드리지 않는다(운영 중 고친 권한을 시작할 때 덮어쓰지 않는다).
func (p *postgres) SeedAccess(ctx context.Context, roles []Role, perms []Perm, users []User) (bool, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(context.Background())
	// 여러 인스턴스가 동시에 떠도 한 번만 넣는다
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", migrateLockID+1); err != nil {
		return false, err
	}
	seeded := false
	var nRoles, nUsers int
	if err := tx.QueryRow(ctx, "SELECT (SELECT count(*) FROM app_roles), (SELECT count(*) FROM app_users)").Scan(&nRoles, &nUsers); err != nil {
		return false, err
	}
	if nRoles == 0 {
		for _, r := range roles {
			if _, err := tx.Exec(ctx, "INSERT INTO app_roles (code, dept_id, dept, label, duty, sort) VALUES ($1, $2, $3, $4, $5, $6)",
				r.Code, r.DeptID, r.Dept, r.Label, r.Duty, r.Sort); err != nil {
				return false, err
			}
		}
		for _, pm := range perms {
			if _, err := tx.Exec(ctx, "INSERT INTO app_role_permissions (role_code, kind, item) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING",
				pm.RoleCode, pm.Kind, pm.Item); err != nil {
				return false, err
			}
		}
		seeded = true
	}
	if nUsers == 0 {
		for _, u := range users {
			if _, err := tx.Exec(ctx, "INSERT INTO app_users (login_id, name, role_code, password_hash) VALUES ($1, $2, $3, $4)",
				u.LoginID, u.Name, u.RoleCode, u.PasswordHash); err != nil {
				return false, err
			}
		}
		seeded = seeded || len(users) > 0
	}
	return seeded, tx.Commit(ctx)
}

func (p *postgres) ListRoles(ctx context.Context) ([]Role, []Perm, error) {
	rows, err := p.pool.Query(ctx, "SELECT code, dept_id, dept, label, duty, sort FROM app_roles ORDER BY sort, code")
	if err != nil {
		return nil, nil, err
	}
	roles, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Role, error) {
		var x Role
		err := r.Scan(&x.Code, &x.DeptID, &x.Dept, &x.Label, &x.Duty, &x.Sort)
		return x, err
	})
	if err != nil {
		return nil, nil, err
	}
	rows, err = p.pool.Query(ctx, "SELECT role_code, kind, item FROM app_role_permissions ORDER BY role_code, kind, item")
	if err != nil {
		return nil, nil, err
	}
	perms, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Perm, error) {
		var x Perm
		err := r.Scan(&x.RoleCode, &x.Kind, &x.Item)
		return x, err
	})
	return roles, perms, err
}

func scanUser(r pgx.CollectableRow) (User, error) {
	var u User
	err := r.Scan(&u.LoginID, &u.Name, &u.RoleCode, &u.PasswordHash, &u.CreatedAt)
	return u, err
}

const userCols = "u.login_id, u.name, u.role_code, u.password_hash, u.created_at"

func (p *postgres) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := p.pool.Query(ctx, "SELECT "+userCols+" FROM app_users u JOIN app_roles r ON r.code = u.role_code ORDER BY r.sort, u.login_id")
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanUser)
}

func (p *postgres) GetUser(ctx context.Context, loginID string) (User, error) {
	rows, err := p.pool.Query(ctx, "SELECT "+userCols+" FROM app_users u WHERE u.login_id = $1", loginID)
	if err != nil {
		return User{}, err
	}
	u, err := pgx.CollectExactlyOneRow(rows, scanUser)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNoUser
	}
	return u, err
}

// CreateLogin은 로그인 세션을 넣고, 만료된 세션을 함께 지운다.
func (p *postgres) CreateLogin(ctx context.Context, tokenHash, loginID string, expires time.Time) error {
	if _, err := p.pool.Exec(ctx, "DELETE FROM app_logins WHERE expires_at <= now()"); err != nil {
		return err
	}
	_, err := p.pool.Exec(ctx, "INSERT INTO app_logins (token_hash, login_id, expires_at) VALUES ($1, $2, $3)", tokenHash, loginID, expires)
	return err
}

func (p *postgres) LoginUser(ctx context.Context, tokenHash string, now time.Time) (User, error) {
	rows, err := p.pool.Query(ctx, "SELECT "+userCols+" FROM app_logins l JOIN app_users u ON u.login_id = l.login_id WHERE l.token_hash = $1 AND l.expires_at > $2", tokenHash, now)
	if err != nil {
		return User{}, err
	}
	u, err := pgx.CollectExactlyOneRow(rows, scanUser)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNoLogin
	}
	return u, err
}

func (p *postgres) DeleteLogin(ctx context.Context, tokenHash string) error {
	_, err := p.pool.Exec(ctx, "DELETE FROM app_logins WHERE token_hash = $1", tokenHash)
	return err
}
