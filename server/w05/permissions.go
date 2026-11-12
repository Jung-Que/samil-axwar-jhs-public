package w05

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// 부서별 권한(이슈 #43, 사용자 결정 11/13 "권한은 부서별로 나눠").
//
// 권한 단위는 부서다. 영업 부서 안의 임원 권한(영업 임원)은 원본 승인 규정(approval_authority_policy.md
// "C-101 재협상/판가 = 영업 임원")이라 따로 둔다. 경영진(CEO/이사회)은 입찰 참여·투자 승인, 운영 관리(관리자)는
// 기준 세션 시계·초기화와 시뮬레이션 세션 정리를 맡는다.
//
// 사용자 부서는 화면에서 고르는 부서 선택이다(X-W05-Role 헤더, 영문 코드). 시연용이라 롤 체크까지만 하고 로그인·인증은
// 두지 않는다(사용자 결정 11/13). 사건의 Actor(권한을 쓴 부서)와 By(실제 입력한 부서)는 시뮬레이션 대행에서만 다르다.

// ErrForbidden은 사용자 부서에 그 기능 권한이 없다는 뜻이다(HTTP 403).
var ErrForbidden = errors.New("권한 없음")

// ErrNoRole은 쓰기 요청에 사용자 부서가 없다는 뜻이다(HTTP 401).
var ErrNoRole = errors.New("부서를 고른다(X-W05-Role)")

// 화면 기능 권한.
const (
	FeatView        = "view"           // 조회
	FeatSimSession  = "sim.session"    // 시뮬레이션 세션 만들기·분기·시연
	FeatSimAct      = "sim.act"        // 시뮬레이션 세션 대행 입력·모의 가정
	FeatSweep       = "sweep"          // 민감도 격자·세션 비교
	FeatMainClock   = "main.clock"     // 기준 세션 시계
	FeatMainReset   = "main.reset"     // 기준 세션 초기화·기준 세션 시연
	FeatSessionDrop = "session.delete" // 시뮬레이션 세션 삭제
)

var commonFeatures = []string{FeatView, FeatSimSession, FeatSimAct, FeatSweep}

// Department는 권한표 한 줄이다. ID는 사건 actor_role에 쓰는 이름, Code는 헤더에 쓰는 영문 코드다.
type Department struct {
	Code     string   `json:"code"`
	ID       string   `json:"id"`
	Dept     string   `json:"dept"` // 부서
	Label    string   `json:"label"`
	Duty     string   `json:"duty"`     // 책임과 근거
	Events   []string `json:"events"`   // 기준 세션에서 넣을 수 있는 사건
	Replies  []string `json:"replies"`  // 회신하는 확인 요청(E3는 요청 대상 부서만)
	Features []string `json:"features"` // 화면 기능
}

// Departments는 부서별 권한표다(화면도 이 표를 그대로 쓴다).
var Departments = []Department{
	{Code: "sales", ID: "영업", Dept: "영업", Label: "영업",
		Duty:   "RFP 접수·상신·제안 제출, 바이어 회신 입력(바이어 대리), 신규 수주 입력(BPMN 영업 레인)",
		Events: []string{EvRFP, EvForApprove, EvSubmit, EvBuyer, EvNewOrder, EvReply}, Replies: []string{"CR-11"}},
	{Code: "sales-exec", ID: "영업 임원", Dept: "영업", Label: "영업 · 임원",
		Duty:   "기존 고객 재협상·판가(approval_authority_policy: C-101 재협상/판가 = 영업 임원), 이탈 의심 고객 능력 반환 확인(R13)",
		Events: []string{EvReply}, Replies: []string{"CR-09"}},
	{Code: "production", ID: "생산관리", Dept: "생산관리", Label: "생산관리",
		Duty:   "능력 단위·PL-01 증산 수단·Masil 계획 포함 여부·타 라인 전환 조건 회신(BPMN 생산관리 레인)",
		Events: []string{EvReply}, Replies: []string{"CR-01", "CR-02", "CR-03", "CR-10"}},
	{Code: "purchasing", ID: "구매·품질", Dept: "구매·품질", Label: "구매·품질",
		Duty:   "M-B 재계약·처방 재검증 기간 회신(OPT-02 선행조건)",
		Events: []string{EvReply}, Replies: []string{"CR-07"}},
	{Code: "masil-scm", ID: "Masil SCM", Dept: "Masil SCM", Label: "Masil SCM",
		Duty:   "Masil 더마 발주 입고 조정 회신(재고 발주 승인 = SCM, approval_authority_policy)",
		Events: []string{EvReply}, Replies: []string{"CR-08"}},
	{Code: "finance", ID: "재무·전략", Dept: "재무·전략", Label: "재무·전략",
		Duty: "환율·원가·회수기간 검토, 300억 조건 반영(그룹 재무). 지금은 조회·시뮬레이션"},
	{Code: "board", ID: "CEO/이사회", Dept: "경영진", Label: "CEO/이사회",
		Duty:   "입찰 참여·투자 승인, 300억 최종 배분(approval_authority_policy)",
		Events: []string{EvDecision}},
	{Code: "admin", ID: "관리자", Dept: "운영 관리", Label: "운영 관리(관리자)",
		Duty:     "기준 세션 시계·초기화, 시뮬레이션 세션 정리(이후 운영 세션 관리)",
		Events:   []string{EvClock},
		Features: []string{FeatMainClock, FeatMainReset, FeatSessionDrop}},
}

func init() {
	for i := range Departments {
		Departments[i].Features = append(append([]string(nil), commonFeatures...), Departments[i].Features...)
	}
}

// 시스템 내부 행위자(사용자 부서로 고를 수 없음). 시뮬레이터는 #39 이전 모의 가정 사건과의 호환용이다.
const (
	ActorSystem    = "시스템"
	ActorSimulator = "시뮬레이터"
)

// DeptByCode는 헤더 코드로 부서를 찾는다.
func DeptByCode(code string) (Department, bool) {
	for _, d := range Departments {
		if d.Code == strings.TrimSpace(code) {
			return d, true
		}
	}
	return Department{}, false
}

func deptByID(id string) (Department, bool) {
	for _, d := range Departments {
		if d.ID == id {
			return d, true
		}
	}
	return Department{}, false
}

// HasFeature는 부서에 화면 기능 권한이 있는지 본다.
func HasFeature(id, feat string) bool {
	d, ok := deptByID(id)
	return ok && slices.Contains(d.Features, feat)
}

// eventActorAllowed는 사건 종류를 그 행위자(부서)가 넣을 수 있는지 본다(모든 세션·재적용에 적용).
// E3 회신은 요청 대상 부서인지 따로 본다(applyReply). 내부 행위자: E00·E0는 시스템, E9는 모든 부서와 시뮬레이터.
func eventActorAllowed(typ, actor string) error {
	switch typ {
	case EvSession:
		if actor == ActorSystem {
			return nil
		}
	case EvClock:
		if actor == ActorSystem {
			return nil
		}
	case EvSimAssumption:
		if actor == ActorSimulator {
			return nil
		}
		if _, ok := deptByID(actor); ok {
			return nil
		}
		return fmt.Errorf("권한 없음: 모의 가정은 부서 사용자만 넣는다(입력 %q)", actor)
	}
	var owners []string
	for _, d := range Departments {
		if slices.Contains(d.Events, typ) {
			if d.ID == actor {
				return nil
			}
			owners = append(owners, d.Label)
		}
	}
	if len(owners) == 0 {
		return nil // 권한표에 없는 사건 종류는 아래에서 "모르는 사건"으로 거절된다
	}
	return fmt.Errorf("권한 없음: %s는 %s 권한이다(입력 %q)", typ, strings.Join(owners, "·"), actor)
}
