---
name: docker-build
description: 로컬 Docker 빌드. (1) dev 테스트 - 배포 이미지(Dockerfile)를 빌드해 Postgres와 함께 띄워 확인하고 모두 내린다(포트 없음). server/store/·server/go.mod·Dockerfile·.dockerignore·axhub.yaml·compose.dev.yaml을 바꿨을 때, 릴리스 직전에 쓴다. (2) 로컬 실행 - 같은 이미지를 Postgres와 함께 계속 띄워 두고 127.0.0.1:8090에서 브라우저로 본다(compose.local.yaml).
---

# Docker 빌드 (dev 테스트와 로컬 실행)

배포와 **같은 이미지**가 Postgres와 함께 뜨는지를 로컬 Docker에서 확인한다. 두 가지 용도가 있다.

| 용도 | 명령 | 포트 | 끝나면 |
|---|---|---|---|
| **dev 테스트** | `python tools/selftest.py devdb` | 없음 | 모두 내림 |
| **로컬 실행**(브라우저 확인) | `compose.dev.yaml` + `compose.local.yaml`, 프로젝트 `axwar-local` | `127.0.0.1:8090`만 | 계속 떠 있음 |

axhub 배포는 axhub 빌드 서버가 하므로 이 빌드는 배포에 필요하지 않다. 배포 전에 깨지는 것을 먼저 잡고, 배포와 같은 구성(Postgres 포함)을 로컬에서 보는 용도다.

## 언제 쓰나

| 상황 | 이 스킬 | 다른 스킬 |
|---|---|---|
| `server/store/`, `server/go.mod`·`server/go.sum`을 바꿨다(저장소·의존성) | 쓴다 | — |
| `Dockerfile`, `.dockerignore`, `axhub.yaml`, `compose.dev.yaml`을 바꿨다 | 쓴다 | — |
| 릴리스 PR 직전 | 쓴다 | `main-build`가 이 스킬을 부른다 |
| 배포와 같은 구성(Postgres)으로 화면을 브라우저로 보고 싶다 | 쓴다(아래 "로컬 실행") | — |
| 화면만 빨리 보고 싶다(DB 없이) | — | `update`의 로컬 실행(Go 서버 직접) |
| 병합된 코드를 받아 로컬을 맞추고 싶다 | 쓰지 않는다 | `update` |

## 지킬 것

- **막을 것은 배포 서버가 이 PC의 localhost에 접속하는 일이다.** 개발용 로컬 포트는 괜찮다(11/10 사용자 확인, 11/09에 적은 "포트를 열지 않는다"를 바로잡음).
- **테스트는 포트 없이 돈다.** `compose.dev.yaml`에 `ports:`를 넣지 않는다. 검사가 설정과 실행 중 테스트 컨테이너 양쪽에서 확인한다.
- **로컬 실행 포트는 `127.0.0.1`에만 연다.** `compose.local.yaml`에 `0.0.0.0`(같은 네트워크의 다른 PC에 공개)을 쓰지 않는다. `devdb` 검사가 확인한다.
- **테스트는 끝나면 모두 내린다.** 테스트 뒤 `axwar-devdb` 컨테이너·네트워크·볼륨이 0개인 것이 정상이다. 남는 것은 빌드한 이미지 `axwar-devdb-app:latest`뿐이다. 로컬 실행(`axwar-local`)은 프로젝트 이름이 달라 테스트가 내리지 않는다.
- DB는 tmpfs라 매번 빈 상태로 시작하고 데이터가 남지 않는다. `compose.dev.yaml`의 비밀번호는 내부 네트워크 전용이다.
- Docker 전역 설정을 바꾸지 않는다. Docker Desktop을 켜고 끄는 것만 한다.

## 절차

1. **엔진 확인.** 출력이 없거나 오류면 Docker Desktop을 켜고 엔진이 뜰 때까지 기다린다(보통 1분 안팎).

   ```powershell
   docker info --format '{{.ServerVersion}}'
   ```

2. **검사 실행.** 한 번에 빌드·기동·확인·정리까지 한다. 첫 실행은 이미지를 받느라 몇 분 걸린다.

   ```powershell
   python tools/selftest.py devdb
   ```

   하는 일(`tools/selftest.py`의 `run_devdb`):

   | 순서 | 내용 |
   |---|---|
   | 0 | `compose.local.yaml`을 합친 설정의 공개 포트가 모두 `127.0.0.1`인지 |
   | 1 | 테스트 설정(`compose.dev.yaml`)에 호스트 공개 포트가 0개인지 |
   | 2 | `test` 서비스: Go 컨테이너가 `server/`를 읽기 전용으로 붙여 Postgres 통합 테스트(`TEST_DATABASE_URL`) |
   | 3 | `app` 서비스: `Dockerfile` 3단계(화면 빌드 → Go 빌드 → alpine 실행)로 이미지를 빌드해 Postgres 모드로 띄우고 healthcheck 대기 |
   | 4 | `smoke` 서비스: 같은 내부 네트워크에서 `/healthz`, `/api/status`(`"store":"postgres"`, `"db":"ok"`, 스키마 버전) 호출 |
   | 5 | 실행 중 컨테이너의 공개 포트가 0개인지 |
   | 6 | `down -v --remove-orphans`로 모두 내림(실패해도 실행) |

3. **결과 확인.** 9개 통과가 정상이다. 결과 요약을 PR 자체 리뷰 댓글에 붙인다.

4. **정리 확인(선택).** 테스트 프로젝트(`axwar-devdb`)가 남아 있지 않으면 정상이다. 로컬 실행(`axwar-local`)은 따로 떠 있을 수 있다.

   ```powershell
   docker compose ls -a; docker ps -a
   ```

## 로컬 실행 (계속 띄워 두고 브라우저로 보기)

배포 이미지와 Postgres를 함께 띄운다. 같은 명령을 다시 실행하면 코드가 바뀐 경우 이미지를 새로 빌드해 앱 컨테이너만 바꾼다(업데이트).

```powershell
docker compose -f compose.dev.yaml -f compose.local.yaml -p axwar-local up -d --build --wait app
```

- 주소: http://127.0.0.1:8090 (화면·API). 포트를 바꾸려면 먼저 `$env:AXWAR_LOCAL_PORT = '9000'`
- 확인: `Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8090/api/status` → `"store":"postgres"`, `"db":"ok"`
- 시연 데이터: 화면 상단 시연 버튼, 또는 `Invoke-WebRequest -UseBasicParsing -Method Post http://127.0.0.1:8090/api/w05/scenarios/normal`
- 상태·로그:

  ```powershell
  docker compose -f compose.dev.yaml -f compose.local.yaml -p axwar-local ps
  docker compose -f compose.dev.yaml -f compose.local.yaml -p axwar-local logs app
  ```

- DB는 tmpfs다. DB 컨테이너를 내리거나 재시작하면 사건 로그가 비고, 앱 컨테이너만 바꾸면 남는다.
- 끄기:

  ```powershell
  docker compose -f compose.dev.yaml -f compose.local.yaml -p axwar-local down -v
  ```

## 단계별로 직접 볼 때(실패 원인 찾기)

검사가 실패해 로그를 봐야 할 때만 쓴다. 마지막 `down`을 빼먹지 않는다.

```powershell
docker compose -f compose.dev.yaml config --format json   # 설정 확인(ports 없음)
docker compose -f compose.dev.yaml run --rm test          # Postgres 통합 테스트
docker compose -f compose.dev.yaml up -d --build --wait app
docker compose -f compose.dev.yaml logs app               # 서버 시작 로그(원본 해시·AI 모드·저장소)
docker compose -f compose.dev.yaml run --rm smoke         # 내부 네트워크에서 호출
docker compose -f compose.dev.yaml down -v --remove-orphans
```

## 실패할 때

| 증상 | 원인·대처 |
|---|---|
| "Docker 엔진이 꺼져 있다" | Docker Desktop을 켜고 `docker info`가 버전을 낼 때까지 기다린 뒤 다시 실행 |
| "호스트에 포트를 공개하는 서비스" | `compose.dev.yaml`에 `ports:`가 들어갔다. 지운다(포트는 `compose.local.yaml`에만) |
| "127.0.0.1이 아닌 공개 포트" | `compose.local.yaml`의 포트에 `127.0.0.1:`이 빠졌다. `"127.0.0.1:${AXWAR_LOCAL_PORT:-8090}:8080"`으로 되돌린다 |
| 로컬 실행에서 8090을 이미 쓰고 있다 | `$env:AXWAR_LOCAL_PORT`로 다른 포트를 주고 다시 `up` |
| Postgres 통합 테스트 실패 | `server/store/`의 migration이나 쿼리 문제. `run --rm test` 출력을 본다. 이미 적용한 migration은 고치지 말고 뒤에 추가한다 |
| 이미지 빌드 실패 | `Dockerfile`이 복사하는 경로와 `.dockerignore`(server/·web/만 보냄)를 확인한다. 새 폴더를 쓰면 둘 다 고친다 |
| healthcheck 통과 못 함 | `logs app`을 본다. "W05 원본 적재 실패"면 `server/w05/data/raw/` 복사본이 `SHA256SUMS`와 다르다 → `python tools/selftest.py engine` |
| `"db":"error"` | 앱이 DB에 붙지 못했다. `logs app`의 "DB 응답 대기" 줄을 본다 |

## 관련

- 설정: `compose.dev.yaml`(테스트), `compose.local.yaml`(로컬 실행 포트), `Dockerfile`, `axhub.yaml`
- 검사 정의: `docs/테스트_하네스.md` §3 `devdb`
- 병합 뒤 로컬 맞추기·브라우저 확인: `docs/skills/update/SKILL.md`
- 배포: `docs/skills/main-build/SKILL.md`
