import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";

// 開発時は Go 側の API サーバへ中継し、ビルド成果物は埋め込み先へ出力する。
// 中継先は環境変数 SD_VIEWER_API で変えられる。
export default defineConfig(({ mode }) => ({
  plugins: [react()],
  build: {
    outDir: "../internal/server/webui/dist",
    // 埋め込み先を空にすると、未ビルドでもビルドが通るための目印まで消えてしまう。
    // 出力前の掃除は Makefile の clean-web で行う。
    emptyOutDir: false,
  },
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: loadEnv(mode, process.cwd(), "SD_VIEWER").SD_VIEWER_API ?? "http://127.0.0.1:8080",
        changeOrigin: true,
      },
    },
  },
}));
