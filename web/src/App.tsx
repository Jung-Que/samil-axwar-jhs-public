import { useEffect, useState } from "react";

type Status = { app: string; stage: string };

// 골격 단계: 화면 → Go API 연결이 살아 있는지만 보여준다.
export default function App() {
  const [status, setStatus] = useState<Status | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    fetch("/api/status")
      .then((r) => (r.ok ? r.json() : Promise.reject(new Error(`HTTP ${r.status}`))))
      .then(setStatus)
      .catch((e: Error) => setError(e.message));
  }, []);

  return (
    <main>
      <h1>AX 인재전쟁 배포 테스트</h1>
      <p className="status">
        {error ? `API 연결 실패: ${error}` : status ? `API 연결됨 · ${status.stage}` : "API 확인 중…"}
      </p>
    </main>
  );
}
