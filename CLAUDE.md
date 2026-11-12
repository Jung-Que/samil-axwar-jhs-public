# 삼일PwC AX 인재전쟁 2기 — 작업 지침

## 프로젝트 목표

Aurora Holdings(가상 화장품 그룹) 데이터로 **기업의 AI 기반 운영 시스템**을 만들어 실제로 돌아가는 모습을 시연한다.
핵심 업무 하나를 골라 `사건 입력 → 데이터 확인 → 판단(엔진 계산 + AI 근거) → 사람 승인 → 작업·계획·상태 갱신 →
다음 사건이 그 상태를 읽음`으로 이어지는 이벤트 루프로 구현한다. 범용 플랫폼도, 분석 보고서도 아니다.
출제·채용은 삼일PwC(FDE 직무). 제출 마감 **2026-11-15(일) 23:59**.
핵심 업무는 **W05 대형 수주와 기존 고객 약속**(Corevia 영업·생산관리)으로 정했다(11/09). 계획: `analysis/06_W05_수주약속/04_기초_플랜.md`

## 진행 중인 작업

- 분석·기획 진행 상황은 `analysis/TODO.md`(git 밖) 맨 위 "지금 하는 일"에 있다. 세션을 시작하면 먼저 읽는다.
- 개발 진행 상황은 열린 이슈·PR이다(`gh pr list --state open`). 앱은 W05 플랫폼 초안이다(#16, #22): 결정 모델 엔진·사건 루프·시연 3건·화면(`server/w05/`, `web/src/`). 입력은 고정 필드만 쓰고 자유 문장·LLM은 현재 제외한다(결합 지점은 사용자가 추후 결정). 명세는 `analysis/06_W05_수주약속/05_업무모델_명세.md`.
- 지금은 dev 단계다(main 릴리스·전역 설정 변경 없음, 확인은 로컬 Docker `axwar-local`). 시뮬레이션 플랫폼으로 만드는 중이고(세션·분기·비교 #38, 이어서 가정 스위치·원본 시계), 이후 서비스 플랫폼으로 발전시킨다. 사건 로그는 세션별이다(`main` = 기준 세션, 이후 운영 세션). 제안: `analysis/06_W05_수주약속/08_시뮬레이션_개선_제안.md`
- 권한은 부서별이다(#44, `server/w05/permissions.go`): 사용자 부서는 화면에서 고르는 부서 선택(`X-W05-Role`). 시연용이라 롤 체크까지만 하고 로그인·인증은 두지 않는다(11/13). 기준 세션은 자기 부서 권한으로만 입력, 시뮬레이션 세션은 대행 입력을 기록하며 허용, 기준 세션 초기화·세션 삭제는 운영 관리만.

## 필수로 알아야 할 사항

### 작업 범위 규칙 (대회 기록)

이 폴더에서 연 세션은 프롬프트와 도구 실행 결과(읽은 파일 내용, 명령 출력 포함)가
대회 기록 서버로 전송된다(`.claude` · `.codex` · `.agents` 훅). 전송 대상은 이 폴더의
대회 자료와 이 폴더에서 한 작업으로 한정한다.

- 이 폴더(`C:\삼일PwC_프로젝트폴더`) 밖의 사용자 파일, 다른 프로젝트 폴더는 읽거나 검색하지 않는다.
- 다른 세션 기록(`~/.claude/projects`, `~/.codex/sessions`, 대화 기록 DB 등)은 열지 않는다.
- 전역 설정·인증 파일(`~/.claude`, `~/.codex`, `auth.json`, `.env`, 키·토큰 파일)은 열지 않는다.
- 위 항목이 꼭 필요하면 먼저 사용자에게 묻고, 허락받은 범위만 다룬다.
- 작업 파일은 이 폴더 안에 둔다. 원본 데이터(`01_참가자_배포용/04_원본데이터`)는 수정하지 않는다.
- `.claude` · `.codex` · `.agents` 폴더와 `.gitignore`는 수정하거나 지우지 않는다(대회 기록 키 포함).
- 전역 도구·플러그인 설치나 전역 설정 변경은 사용자 확인 없이 하지 않는다.

### 과제 완료 기준 (문제지 A01·A04)

- BI·보고서·권고문만으로는 미완료다. 결정·회신에 따라 작업·계획·상태가 실제로 생성·갱신돼야 한다.
- 필수 시연 3건: 정상 / 변경(조건·가정이 바뀐 뒤 재검증) / 정보 부족 또는 승인 거절.
- 필수 확장 1개와 다른 부서 목표·제약 영향 1개를 실제 처리에 반영한다(같은 사례로 함께 충족 가능).
- 실제 작동 · 모의 처리 · 설계 단계를 구분해 표시한다.

### 데이터 규칙 (문제지 A05)

- CSV는 UTF-8, 헤더 1행, **파이프 `|` 구분자**. 금액은 KRW 정수·VAT 제외, 제품 EA, 원료 kg.
- `NONE`은 "해당 없음" 코드이며 0이 아니다. 업무 기준일 2026-07-01, 월~금 영업일.
- 원본 기록 · 부서 주장 · 참가자 가정 · 합성 자료 · 계산 결과를 구분해 표시한다.
- 원본 제약(생산능력·계약·승인권한)을 가정으로 없애지 않는다. 바꾸려면 대안 시나리오로 분리한다.
- 회사 간 실제 키는 `m_sku_ms.product_id → m_product_cv` 하나뿐이다. 품질 출하(SBL)↔매출 출하(SH), 원료 PO↔로트는 키가 없으니 매핑하면 "합성"으로 표시한다.

### 앱·배포

- **서버 Go**(`server/`, 1.26, 표준 라이브러리 + Postgres 드라이버 `pgx`만) · **화면 React + TypeScript**(`web/`, **Vite** 빌드). Go 서버가 `/api/*`와 빌드된 화면(`WEB_DIR`)을 함께 제공한다.
- 로컬 실행: `go -C server run .`(`127.0.0.1:8080`, DB 없으면 메모리 저장소) + `cd web && npm ci && npm run dev`(Vite가 `/api`를 8080으로 넘김). Postgres는 로컬 Docker dev 테스트 `python tools/selftest.py devdb`(`compose.dev.yaml`, 호스트 포트 공개 없음)로 보고, 로컬 Docker로 띄워 브라우저로 볼 때는 `compose.local.yaml`(`127.0.0.1:8090`만)을 덧붙인다. 막을 것은 배포 서버가 이 PC의 localhost에 접속하는 일이다(개발용 로컬 포트는 괜찮다, 11/10). 의존성은 `server/go.sum`·`web/package-lock.json`으로 고정하고, 새로 넣으면 PR에 이유를 적는다.
- axhub 배포(`axhub.yaml`), 헬스체크 `/healthz`, 포트는 `PORT` 환경변수, DB는 앱 전용 Postgres(`DATABASE_URL` 주입). `Dockerfile`은 3단계(화면 빌드 → Go 빌드 → alpine 실행), `.dockerignore`는 `server/`·`web/`만 보낸다. 새 폴더를 쓰면 둘 다 고친다. W05 원본 복사본은 `server/w05/data/raw/`(바이너리에 포함, 손으로 고치지 않음). LLM(AI API)은 현재 제외라 키를 넣지 않는다.
- 소스 zip 제출 시 `.claude` · `.codex` · `.agents`와 `analysis/03_1기_수상작/videos/`는 뺀다.

### Git 작업 흐름 (CI/CD 없음)

**개발 작업**(앱·도구·배포 설정·개발 규칙 문서)은 **이슈 → 드래프트 PR → 작업 → 자체 리뷰 → PR 종료** 순서다. 이슈·PR은 GitHub 원격 `github`(비공개, https://github.com/Jung-Que/samil-axwar-jhs), 배포는 axhub 원격 `origin`.

- **개발 외 작업은 이슈·PR을 만들지 않는다.** 리서치·문제 분석·기획·분석 스크립트·할 일은 `analysis/`(git 제외, `.git/info/exclude`)에서 바로 고치고 `python tools/selftest.py analysis`로 확인한다. 구분: `docs/브랜치_규칙.md` §0
- **브랜치:** `main` = axhub 배포본, `dev` = 통합(GitHub 기본 브랜치). 작업 브랜치는 **항상 `dev`에서** `<종류>/<이슈번호>-<영문-slug>`로 만든다(예: `feat/7-engine-v1`). `main`·`dev`에는 직접 커밋·푸시하지 않는다. 전체 규칙·명령: `docs/브랜치_규칙.md`

1. **이슈:** `gh issue create` — 배경·범위·완료 기준·돌릴 검사 그룹을 적는다.
2. **드래프트 PR:** `git switch dev` → `git pull` → `gh issue develop <번호> --checkout --base dev --name <종류>/<번호>-<slug>` → 빈 커밋 `chore: #<번호> 작업 시작` → push → `gh pr create --draft --base dev`(본문 첫 줄 `Closes #<번호>`).
3. **작업:** 의미 단위로 커밋한다. 메시지는 `<종류>: <내용>`(feat·fix·docs·data·chore·test).
4. **자체 리뷰:** `git diff dev...HEAD`를 다시 읽고 `docs/테스트_하네스.md` 분기표가 고른 그룹만 `python tools/selftest.py <그룹>`으로 돌린다. 결과·발견·수정을 PR 리뷰 댓글로 남기고 `gh pr ready`.
5. **PR 종료:** `gh pr merge --squash --delete-branch` → `git switch dev` → `git pull`.

- **배포(릴리스):** 앱이 바뀌었거나 제출 전에만 `dev` → `main` 릴리스 PR을 merge commit으로 병합하고(`--delete-branch` 금지) `git push origin main`(자동 배포). 절차: `docs/브랜치_규칙.md` §4
- 로컬 pre-push 훅(`tools/git-hooks/pre-push`, 설치 `git config core.hooksPath tools/git-hooks`)이 규칙 위반 푸시를 막는다. `--no-verify`는 사용자 확인 뒤에만.
- GitHub Actions는 꺼 두었고 CI/CD를 추가하지 않는다. 새 영역·검사가 생기면 그 PR에서 하네스 문서와 `tools/selftest.py`를 함께 고친다.

## 정보 위치

| 필요한 정보 | 위치 |
|---|---|
| 과제 지시(A01 문제지 ~ A05 데이터 기준) | `01_참가자_배포용/참가자_문제지와_가이드.pdf` (HTML: `A01.html`~`A05.html`) |
| 세계관별 현재 업무 BPMN | `01_참가자_배포용/세계관_BPMN/W01~W08`, `01_참가자_배포용/A06_세계관별_BPMN_도식집.pdf` |
| 원본 데이터 전체 | `01_참가자_배포용/04_원본데이터/` (파일 목록: `01_참가자_배포용/데이터_파일목록.html`) |
| 데이터 사전·계산 정의 | `04_원본데이터/00_common/data_dictionary.md`, `calculation_definitions.md` |
| 승인권한·업무 흐름 | `04_원본데이터/00_common/approval_authority_policy.md`, `Business_Process_Map.md` |
| 8개 세계관 질문이 나온 이유 | `analysis/05_데이터_연결/01_질문의_기원.md` |
| 데이터 지도(파일·키·일치율·A05 한계 수치) | `analysis/05_데이터_연결/02_데이터_지도.md` |
| 세계관 연결 허브·영향 경로 | `analysis/05_데이터_연결/03_세계관_연결_지도.md` |
| 세계관별 상세(기원·흐름·객체·공백·후보성) | `analysis/05_데이터_연결/세계관/W01~W08_*.md` |
| 관심 부서 3순위 추천(주제 선택 근거) | `analysis/05_데이터_연결/04_관심_부서_추천.md` |
| W05 기초 플랜(이벤트 루프·업무모델 초안·시연·일정·결정 사항) | `analysis/06_W05_수주약속/04_기초_플랜.md` |
| W05 문제 상황 분석(시간표·이해관계자·갈등·오판 함정·결정 순서·비교 지점) | `analysis/06_W05_수주약속/03_문제상황_분석.md` |
| W05 도메인 지식·관련 DB(재현 스크립트 포함) | `analysis/06_W05_수주약속/` |
| 개발 외 작업 폴더(리서치·분석·기획) 목록과 핵심 결론 | `analysis/README.md` |
| 결과물 형태·범위 결정 | `analysis/02_과제_해석/결과물_형태_결정.md` |
| 1기 우승작 분석(같은 데이터룸) | `analysis/03_1기_수상작/수상작_분석.md` |
| 출제사 삼일PwC 조사 | `analysis/01_주최사_삼일PwC/삼일PwC_조사.md` |
| 회수·현금·공급 실제 사례 | `analysis/04_운영사례조사/` |
| 자체 테스트 분기(무엇을 바꾸면 무엇을 돌리나) | `docs/테스트_하네스.md` (실행기 `tools/selftest.py`) |
| 브랜치 규칙(dev·main·이름·릴리스·훅) | `docs/브랜치_규칙.md` |
| 용도별 실행 절차: Docker 빌드·병합 뒤 업데이트·메인 빌드(릴리스·배포) | `docs/skills/docker-build/SKILL.md`, `docs/skills/update/SKILL.md`, `docs/skills/main-build/SKILL.md` |
| 할 일·지금 하는 일 | `analysis/TODO.md` |

## 이 파일 관리

- **100줄 이내**를 유지한다("진행 중인 작업" 섹션은 줄 수에서 뺀다). 긴 정보는 여기 쓰지 말고 파일로 만든 뒤 위 표에 경로만 추가한다.
  줄 수와 표의 경로는 `python tools/selftest.py docs`가 확인한다.
- 작업을 끝내면 `analysis/TODO.md`를, `analysis/`에 파일을 추가하면 `analysis/README.md`를 갱신한다.
- 새 일은 시작일과 함께 `analysis/TODO.md` "지금 하는 일"에 두고, 끝나면 `[x]`와 날짜로 옮긴다.
  이 파일의 "진행 중인 작업"은 앱 상태가 바뀌는 개발 PR에서만 고친다.
