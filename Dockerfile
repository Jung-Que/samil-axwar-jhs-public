# 1단계: 화면(React + Vite) 빌드
FROM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# 2단계: 서버(Go, 표준 라이브러리만) 빌드
FROM golang:1.26-alpine AS server
WORKDIR /src
COPY server/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server .

# 3단계: 실행 이미지 — 서버 바이너리와 빌드된 화면만 담는다
FROM alpine:3.22
WORKDIR /app
COPY --from=server /out/server ./server
COPY --from=web /web/dist ./web
ENV PORT=8080 WEB_DIR=/app/web
EXPOSE 8080
USER nobody
CMD ["./server"]
