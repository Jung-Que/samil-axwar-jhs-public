# 1단계: 화면(React + Vite) 빌드
FROM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# 2단계: 서버(Go, 표준 라이브러리 + Postgres 드라이버 pgx) 빌드
FROM golang:1.26-alpine AS server
WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server .

# 3단계: 실행 이미지 — 서버 바이너리와 빌드된 화면만 담는다
FROM alpine:3.22
WORKDIR /app
COPY --from=server /out/server ./server
COPY --from=web /web/dist ./web
# 컨테이너는 바깥(axhub 라우터)에서 들어와야 하므로 0.0.0.0에 연다. 로컬 실행 기본값은 127.0.0.1이다.
ENV PORT=8080 WEB_DIR=/app/web HOST=0.0.0.0
EXPOSE 8080
USER nobody
CMD ["./server"]
