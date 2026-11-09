import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// 개발 중(npm run dev)에는 API를 Go 서버(go -C server run .)로 넘긴다.
// 배포에서는 Go 서버가 빌드된 dist/를 직접 제공하므로 프록시가 없다.
const api = "http://localhost:8080";

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      "/api": api,
      "/healthz": api,
    },
  },
  build: {
    outDir: "dist",
  },
});
