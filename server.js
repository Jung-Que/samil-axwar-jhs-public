// AX 인재전쟁 앱 서버 — Node 기본 모듈만 사용해요.
// axhub 는 앱이 열 포트를 PORT 환경변수로 넣어 주고, 바깥(파드 주소)에서 상태를 확인해요.
const http = require('node:http');
const fs = require('node:fs');
const path = require('node:path');

const PORT = Number(process.env.PORT) || 8080;
const HOST = '0.0.0.0';

const indexHtml = fs.readFileSync(path.join(__dirname, 'public', 'index.html'));

function send(req, res, status, type, body) {
  res.writeHead(status, { 'Content-Type': type });
  res.end(req.method === 'HEAD' ? undefined : body);
}

const server = http.createServer((req, res) => {
  if (req.method !== 'GET' && req.method !== 'HEAD') {
    res.writeHead(405, { Allow: 'GET, HEAD' });
    return res.end();
  }

  const { pathname } = new URL(req.url, 'http://localhost');

  // 상태 확인 전용 경로: 로그인·DB 없이 바로 200 을 줘요.
  if (pathname === '/healthz') {
    return send(req, res, 200, 'text/plain; charset=utf-8', 'ok');
  }

  if (pathname === '/' || pathname === '/index.html') {
    return send(req, res, 200, 'text/html; charset=utf-8', indexHtml);
  }

  return send(req, res, 404, 'text/plain; charset=utf-8', 'Not Found');
});

server.listen(PORT, HOST, () => {
  console.log(`listening on http://${HOST}:${PORT}`);
});

// 재배포·재시작 때 연결을 정리하고 끝내요.
for (const signal of ['SIGTERM', 'SIGINT']) {
  process.on(signal, () => server.close(() => process.exit(0)));
}
