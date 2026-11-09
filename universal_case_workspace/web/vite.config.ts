/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The BFF is proxied in development so the browser talks to a single origin (no CORS, no tokens in the browser).
const apiTarget = process.env.UCW_API_PROXY ?? "http://127.0.0.1:8480";

// Behind the Control Plane the SPA is mounted at /apps/case-workspace/ (production build default).
// The dev server runs at / unless UCW_BASE_PATH says otherwise; API and router paths derive from BASE_URL.
const mountPath = "/apps/case-workspace/";

export default defineConfig(({ command }) => {
  const basePath = process.env.UCW_BASE_PATH ?? (command === "serve" ? "/" : mountPath);
  if (!/^\/([a-z][a-z0-9-]*\/)*$/.test(basePath)) throw new Error(`UCW_BASE_PATH must look like /apps/case-workspace/, got ${basePath}`);
  const apiPrefix = `${basePath.replace(/\/$/, "")}/api`;
  return {
    base: basePath,
    plugins: [react()],
    server: {
      host: "127.0.0.1",
      port: 5174,
      strictPort: true,
      proxy: {
        // The Control Plane strips the mount path before the BFF; do the same here.
        [apiPrefix]: { target: apiTarget, changeOrigin: false, rewrite: (p: string) => p.slice(apiPrefix.length - "/api".length) },
      },
    },
    build: { sourcemap: true },
    test: {
      environment: "jsdom",
      globals: true,
      setupFiles: ["./src/test/setup.ts"],
      restoreMocks: true,
    },
  };
});
