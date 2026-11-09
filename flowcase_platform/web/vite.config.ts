/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The API is proxied in development so the browser talks to a single origin (no CORS, no cookies).
const apiTarget = process.env.FLOWCASE_API_PROXY ?? "http://127.0.0.1:8080";

// Optional mount path when served behind the AI Workspace Control Plane (e.g. /apps/flowcase/).
// Defaults to "/" so the standalone deployment is unchanged.
const basePath = process.env.FLOWCASE_BASE_PATH ?? "/";
if (!/^\/([a-z][a-z0-9-]*\/)*$/.test(basePath)) throw new Error(`FLOWCASE_BASE_PATH must look like /apps/flowcase/, got ${basePath}`);

export default defineConfig({
  base: basePath,
  plugins: [react()],
  server: {
    host: "127.0.0.1",
    port: 5173,
    strictPort: true,
    proxy: {
      "/api": { target: apiTarget, changeOrigin: false },
      "/readyz": { target: apiTarget },
      "/healthz": { target: apiTarget },
    },
  },
  build: { sourcemap: true },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
    restoreMocks: true,
  },
});
