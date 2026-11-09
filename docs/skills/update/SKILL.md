---
name: update
description: PR 병합 뒤 로컬을 최신으로 맞추고 실행해 확인한다. dev를 받고, 의존성을 맞추고, 검사를 돌리고, Docker 없이 로컬에서 서버·화면을 띄워 브라우저로 본다. 새 세션을 시작할 때, 다른 세션이 PR을 병합한 뒤, 화면을 직접 보고 싶을 때, W05 원본 복사본을 다시 만들어야 할 때 쓴다. 배포(main)는 다루지 않는다.
---

# 업데이트 (병합 뒤 로컬 최신화와 로컬 실행)

## 언제 쓰나

| 상황 | 할 절차 |
|---|---|
| 새 세션 시작, 다른 세션·사람이 PR을 병합함 | 1 → 2 → 3 |
| 화면을 브라우저로 보고 싶다 | 4 (Postgres까지 배포와 같게 보려면 `docker-build` 스킬 "로컬 실행") |
| `Dockerfile`·저장소가 바뀌어 이미지를 다시 확인해야 한다 | 5 (`docker-build` 스킬) |
| W05 원본이 바뀌었다(드묾) | 6 |

## 1. 코드 최신화

1. 지금 상태를 본다. **바꾸지 않은 작업이 있거나 다른 브랜치에 있으면 멈추고 확인한다.** 다른 세션이 작업 브랜치를 체크아웃해 둔 채일 수 있다(11/10 PR #14 사례). 그 경우 브랜치를 바꾸기 전에 사용자에게 묻는다.

   ```powershell
   git status --short; git branch --show-current; gh pr list --state open
   ```

2. `dev`를 받는다. 원격 이름은 `github`(GitHub)와 `origin`(axhub)이다. `dev`는 `github`에서만 받는다.

   ```powershell
   git switch dev; git pull github dev
   ```

3. 바뀐 파일을 보고 다음 단계를 고른다.

   ```powershell
   git diff --name-only "HEAD@{1}" HEAD
   ```

## 2. 의존성 맞추기

| 바뀐 파일 | 명령 |
|---|---|
| `server/go.mod`, `server/go.sum` | `go -C server mod download` |
| `web/package.json`, `web/package-lock.json` | `cd web; npm ci` (잠금 파일 그대로 설치) |

새 의존성을 넣는 것은 개발 PR에서만 하고 이유를 PR에 적는다(CLAUDE.md 앱·배포).

## 3. 검사

```powershell
python tools/selftest.py app engine docs guard
```

- `app`: Go 형식·vet·테스트·빌드, 화면 빌드, 서버 기동 확인
- `engine`: W05 원본 복사본 해시, 업무모델 기대값, 시연 3건
- 저장소·이미지가 바뀌었으면 `docker-build` 스킬(`devdb`)도 돌린다

## 4. 로컬 실행 (브라우저 확인)

두 가지 방법이 있다.

| 방법 | 저장소 | 주소 | 쓰는 때 |
|---|---|---|---|
| Go 서버 직접(아래) | 메모리 | http://127.0.0.1:8080, 개발 모드 http://localhost:5173 | 화면을 고치며 바로 볼 때 |
| 로컬 Docker(`docker-build` 스킬 "로컬 실행") | Postgres | http://127.0.0.1:8090 | 배포와 같은 구성으로 볼 때 |

Go 서버 직접 실행은 `127.0.0.1`에만 열린다(이 PC 안). DB 주소가 없으면 **메모리 저장소**라 서버를 끄면 사건 로그가 사라진다.

**개발 모드(화면 수정을 바로 보며):** 터미널 두 개.

```powershell
go -C server run .            # API: http://127.0.0.1:8080
cd web; npm run dev           # 화면: http://localhost:5173 (Vite가 /api를 127.0.0.1:8080으로 넘김)
```

**빌드본 한 프로세스(배포와 같은 구성):** Go 서버가 `web/dist`를 함께 제공한다.

```powershell
cd web; npm run build; cd ..
go -C server run .            # 화면과 API: http://127.0.0.1:8080
```

- 시연: 화면 상단의 정상·변경·정보 부족·거절 버튼. 탭은 `#capacity`, `#ledger`, `#requests`, `#log`처럼 주소 해시로도 연다.
- API로 돌릴 때: `Invoke-RestMethod -Method Post http://127.0.0.1:8080/api/w05/scenarios/normal`
- AI: 키가 없으면 녹화 응답([모의])과 폼 입력으로 돈다. 실제 API 결합 지점은 사용자가 정하기 전까지 키를 넣지 않는다(11/10).
- 끄기: 각 터미널에서 Ctrl+C. 백그라운드로 띄웠다면 그 프로세스를 끝낸다.

## 5. 이미지 업데이트

`Dockerfile`, `.dockerignore`, `axhub.yaml`, `server/store/`, `server/go.mod`가 바뀌었으면 `docker-build` 스킬의 dev 테스트(`devdb`)를 돌린다. `devdb`는 매번 `--build`로 이미지를 새로 만들고 끝나면 모두 내린다.

로컬 Docker로 띄워 둔 앱(`axwar-local`)이 있으면, 병합 뒤 같은 `up -d --build --wait app` 명령을 다시 실행해 새 코드로 바꾼다(`docker-build` 스킬 "로컬 실행").

## 6. W05 원본 복사본 다시 만들기

앱은 원본을 `server/w05/data/raw/`에 바이트 그대로 복사해 바이너리에 넣는다. 대회 원본(`01_참가자_배포용/04_원본데이터`)이 바뀌었거나 앱이 읽는 파일을 늘릴 때만 한다. **개발 작업이라 이슈·PR(`data/<번호>-…` 브랜치)로 한다.**

1. 손으로 고치지 않는다. 원본에서 다시 복사하고 `SHA256SUMS`를 새로 만든다(경로는 원본 기준 상대 경로, 줄바꿈 LF).

   ```powershell
   $src = '01_참가자_배포용\04_원본데이터'; $dst = (Resolve-Path 'server\w05\data\raw').Path
   $rels = Get-Content "$dst\SHA256SUMS" | ForEach-Object { ($_ -split '  ', 2)[1] }   # 파일을 늘리면 여기에 더한다
   $lines = foreach ($r in $rels) {
     Copy-Item -LiteralPath (Join-Path $src $r) -Destination (Join-Path $dst $r) -Force
     "$((Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $dst $r)).Hash.ToLower())  $r"
   }
   [IO.File]::WriteAllText("$dst\SHA256SUMS", (($lines -join "`n") + "`n"), (New-Object Text.UTF8Encoding $false))
   ```

   PowerShell의 `[IO.File]`은 현재 위치가 아니라 프로세스 작업 폴더를 기준으로 하므로 위처럼 절대 경로(`Resolve-Path`)를 쓴다.

2. `python tools/selftest.py engine`으로 복사본 = `SHA256SUMS` = 원본과 기대값을 확인한다. 기대값이 바뀌면 업무모델 명세(`analysis/06_W05_수주약속/05_업무모델_명세.md`, git 밖)를 먼저 고친다.

## 하지 말 것

- `main`·`dev`에 직접 커밋·푸시(훅이 막는다). 고칠 것이 있으면 작업 브랜치를 만든다.
- `analysis/`를 덮어쓰거나 지우기(git 밖이라 되돌릴 수 없다).
- 원본(`01_참가자_배포용/`) 수정.

## 관련

- 브랜치·작업 흐름: `docs/브랜치_규칙.md`
- 검사 분기: `docs/테스트_하네스.md`
- Docker 빌드: `docs/skills/docker-build/SKILL.md`
- 배포: `docs/skills/main-build/SKILL.md`
