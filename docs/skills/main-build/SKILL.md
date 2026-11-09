---
name: main-build
description: dev → main 릴리스와 axhub 배포. 릴리스 PR(merge commit)로 main을 맞추고 axhub(origin)에 main을 올려 배포한 뒤, 배포 상태·사이트를 확인하고 필요하면 되돌린다. 앱이 바뀌어 사이트에 올릴 때와 제출 전 마지막 빌드에 쓴다. 외부 공개 사이트가 바뀌므로 사용자 확인 없이 시작하지 않는다.
---

# 메인 빌드 (`dev` → `main` 릴리스와 axhub 배포)

`main`은 axhub 사이트에 올라간 코드와 같게 유지한다. axhub는 `origin`에 `main`이 올라오면 원격 빌드 서버에서 `Dockerfile`로 빌드해 배포한다(`axhub.yaml`: `deploy_method: docker`, 헬스체크 `/healthz`, 포트 8080, 앱 전용 Postgres).

## 시작 전 반드시

- **사용자에게 배포해도 되는지 묻는다.** 앱은 외부 공개(인터넷)라 배포하면 바로 바뀐다. 앞서 받은 승인은 이번 배포에 넘어오지 않는다.
- 이번에 나갈 변경을 사용자에게 보여 준다(아래 1-2).
- 앱 이름(slug)은 `samil-axwar-jhs`다(`axhub.yaml`의 `name`).

## 1. 사전 점검

1. 최신 `dev`와 나갈 변경:

   ```powershell
   git switch dev; git pull github dev; git fetch github
   git log --oneline github/main..github/dev
   ```

2. 전체 검사와 Docker 빌드:

   ```powershell
   python tools/selftest.py all       # devdb를 뺀 전부
   python tools/selftest.py devdb     # docker-build 스킬. Docker Desktop 필요
   ```

3. axhub 배포 전 확인(읽기 전용). 필요한 환경변수 누락, 커밋·푸시 안 된 변경, 주소(서브도메인) 없음 등을 알려 준다. 막는 것이 있으면 종료 코드 9, `axhub.yaml` 오류면 64.

   ```powershell
   axhub deploy check --app samil-axwar-jhs
   axhub env list --app samil-axwar-jhs
   ```

   - LLM(AI API)은 현재 제외라(11/10) AI 키를 넣지 않는다. 앱은 고정 필드 입력만 받는다.
   - 비밀값은 `axhub env`로만 넣고 코드·이미지·커밋에 넣지 않는다.

## 2. 릴리스 PR

릴리스도 이슈 → 드래프트 PR → 자체 리뷰 → 종료를 따른다(`docs/브랜치_규칙.md` §4).

```powershell
gh issue create --title "release: <날짜> <요약>" --body-file <본문 파일>   # 포함 PR, 확인할 화면
gh pr create --draft --base main --head dev --title "release: <날짜> <요약>" --body-file <본문 파일>   # 본문은 Refs #<이슈>
# 자체 리뷰: git diff github/main...github/dev → 리뷰 댓글 → gh pr ready <PR번호>
gh pr merge <PR번호> --merge          # merge commit. --delete-branch는 붙이지 않는다(dev가 지워진다)
```

- PowerShell 5.1에서 큰따옴표가 든 본문을 `--body`로 넘기면 인자가 쪼개진다. 본문은 파일로 쓰고 `--body-file`로 넘긴다(11/10 PR #16 사례).
- 릴리스 PR은 `main`으로 병합되므로 이슈가 자동으로 닫히지 않는다(본문은 `Closes` 대신 `Refs`).

## 3. 배포

```powershell
git switch main; git pull github main
git push origin main                  # axhub 배포 시작. 훅이 GitHub main과 다른 커밋은 막는다
git ls-remote --heads github dev      # dev가 남아 있는지
git switch dev
```

- 훅을 건너뛰는 `--no-verify`는 사용자 확인 뒤에만 쓴다.
- 푸시만으로 배포가 시작되지 않으면 스테이징 경로를 확인한다. 공개 앱은 스테이징 → 반영 신청 → 승인 경로를 쓸 수 있다(`analysis/02_과제_해석/결과물_형태_결정.md` "결정 뒤 배포·데이터 구성안"). 먼저 부작용 없는 미리보기부터 본다.

  ```powershell
  axhub deploy staging --help; axhub deploy promote-preflight --help
  ```

## 4. 배포 확인

```powershell
axhub deploy status --app samil-axwar-jhs              # 최신 배포 상태와 ID
axhub deploy watch  --app samil-axwar-jhs <배포ID>     # 끝날 때까지 지켜보기
axhub deploy logs   --app samil-axwar-jhs <배포ID>     # 빌드·실행 로그
axhub deploy verify --app samil-axwar-jhs <배포ID>     # 성공 검증
```

사이트(배포 상태에 나오는 주소)에서 확인할 것:

| 주소 | 기대 |
|---|---|
| `/healthz` | 200, `ok` |
| `/api/status` | `"store":"postgres"`, `"db":"ok"`, `"stage":"draft"` |
| `/api/w05/state` | `"raw_files_verified":15` |
| 첫 화면 | 판단 카드와 시연 버튼이 보임 |

- 사이트에서 시연 버튼(시나리오 실행)을 누르면 **공용 상태가 초기화되고 바뀐다.** 확인 뒤 보여 줄 시작 상태(예: 초기화 또는 정상 시나리오)로 맞춰 둔다.
- 결과(배포 ID, 확인 항목)를 릴리스 이슈 댓글로 남기고 `gh issue close <번호>`.

## 5. 되돌리기

| 상황 | 방법 |
|---|---|
| 배포가 실패했거나 사이트가 깨졌다 | `axhub deploy rollback --app samil-axwar-jhs --from-deployment <이전 배포ID>`로 먼저 미리보기(기본이 dry-run) → 사용자 확인 → `--execute` |
| 코드를 되돌려야 한다 | `dev`에서 되돌리는 작업 PR(`fix/…`)을 만들어 병합 → 다시 이 스킬로 릴리스. `main` 강제 푸시는 쓰지 않는다 |

## 6. 제출 전 마지막 빌드

1. 위 1~4로 최종 릴리스를 하고 배포 URL에서 시연 3건을 재현한다.
2. 소스 zip은 `.claude` · `.codex` · `.agents`와 `analysis/03_1기_수상작/videos/`를 빼고 만든다(CLAUDE.md 앱·배포).
3. 제출 문서의 실행·재현 방법에 배포 URL과 로컬 실행(`update` 스킬 4)을 적는다.

## 관련

- 릴리스 규칙·훅: `docs/브랜치_규칙.md` §4·§5
- 검사: `docs/테스트_하네스.md`
- Docker 빌드: `docs/skills/docker-build/SKILL.md`
- 로컬 최신화·실행: `docs/skills/update/SKILL.md`
