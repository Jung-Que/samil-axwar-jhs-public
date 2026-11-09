---
name: docker-build
description: 로컬 Docker dev 테스트 빌드. 배포 이미지(Dockerfile)를 빌드해 Postgres 저장소와 함께 로컬 Docker 안에서 띄워 보고 내린다. server/store/·server/go.mod·Dockerfile·.dockerignore·axhub.yaml·compose.dev.yaml을 바꿨을 때, 또는 릴리스 직전에 쓴다. 호스트 포트를 열지 않으며 끝나면 컨테이너를 모두 지운다. 화면을 브라우저로 보려는 용도가 아니다(그건 update 스킬).
---

# Docker 빌드 (로컬 dev 테스트)

배포와 **같은 이미지**가 Postgres와 함께 뜨는지를 로컬 Docker 안에서 확인한다.
axhub 배포는 axhub 빌드 서버가 하므로 이 빌드는 배포에 필요하지 않다. 배포 전에 깨지는 것을 먼저 잡는 용도다.

## 언제 쓰나

| 상황 | 이 스킬 | 다른 스킬 |
|---|---|---|
| `server/store/`, `server/go.mod`·`server/go.sum`을 바꿨다(저장소·의존성) | 쓴다 | — |
| `Dockerfile`, `.dockerignore`, `axhub.yaml`, `compose.dev.yaml`을 바꿨다 | 쓴다 | — |
| 릴리스 PR 직전 | 쓴다 | `main-build`가 이 스킬을 부른다 |
| 화면을 브라우저로 보고 싶다 | 쓰지 않는다(호스트 포트가 없어 볼 수 없다) | `update`의 로컬 실행 |
| 병합된 코드를 받아 로컬을 맞추고 싶다 | 쓰지 않는다 | `update` |

## 지킬 것

- **호스트 포트를 열지 않는다.** `compose.dev.yaml`에 `ports:`를 넣지 않고, `docker run -p`도 쓰지 않는다(11/09 사용자 조건). 검사가 설정과 실행 중 컨테이너 양쪽에서 확인한다.
- **끝나면 모두 내린다.** 테스트가 끝난 뒤 컨테이너·네트워크·볼륨이 0개인 것이 정상이다. 남는 것은 빌드한 이미지 `axwar-devdb-app:latest`뿐이다.
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
   | 1 | compose 설정에 호스트 공개 포트가 0개인지 |
   | 2 | `test` 서비스: Go 컨테이너가 `server/`를 읽기 전용으로 붙여 Postgres 통합 테스트(`TEST_DATABASE_URL`) |
   | 3 | `app` 서비스: `Dockerfile` 3단계(화면 빌드 → Go 빌드 → alpine 실행)로 이미지를 빌드해 Postgres 모드로 띄우고 healthcheck 대기 |
   | 4 | `smoke` 서비스: 같은 내부 네트워크에서 `/healthz`, `/api/status`(`"store":"postgres"`, `"db":"ok"`, 스키마 버전) 호출 |
   | 5 | 실행 중 컨테이너의 공개 포트가 0개인지 |
   | 6 | `down -v --remove-orphans`로 모두 내림(실패해도 실행) |

3. **결과 확인.** 8개 통과가 정상이다. 결과 요약을 PR 자체 리뷰 댓글에 붙인다.

4. **정리 확인(선택).** 아래가 모두 비어 있으면 정상이다.

   ```powershell
   docker ps -a; docker compose ls -a
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
| "호스트에 포트를 공개하는 서비스" | `compose.dev.yaml`에 `ports:`가 들어갔다. 지운다 |
| Postgres 통합 테스트 실패 | `server/store/`의 migration이나 쿼리 문제. `run --rm test` 출력을 본다. 이미 적용한 migration은 고치지 말고 뒤에 추가한다 |
| 이미지 빌드 실패 | `Dockerfile`이 복사하는 경로와 `.dockerignore`(server/·web/만 보냄)를 확인한다. 새 폴더를 쓰면 둘 다 고친다 |
| healthcheck 통과 못 함 | `logs app`을 본다. "W05 원본 적재 실패"면 `server/w05/data/raw/` 복사본이 `SHA256SUMS`와 다르다 → `python tools/selftest.py engine` |
| `"db":"error"` | 앱이 DB에 붙지 못했다. `logs app`의 "DB 응답 대기" 줄을 본다 |

## 관련

- 설정: `compose.dev.yaml`, `Dockerfile`, `axhub.yaml`
- 검사 정의: `docs/테스트_하네스.md` §3 `devdb`
- 병합 뒤 로컬 맞추기·브라우저 확인: `docs/skills/update/SKILL.md`
- 배포: `docs/skills/main-build/SKILL.md`
