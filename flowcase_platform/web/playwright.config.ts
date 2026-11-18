import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  workers: 1,
  timeout: 30000,
  use: {
    baseURL: "http://127.0.0.1:5173",
    headless: true,
    trace: "retain-on-failure",
  },
  reporter: [
    ["list"],
    [
      "json",
      { outputFile: "../../docs/elenio-handoff/evidence/e2e-results.json" },
    ],
  ],
  webServer: {
    command: "npm run dev",
    url: "http://127.0.0.1:5173/elenio.html",
    reuseExistingServer: true,
  },
  outputDir: "test-results",
});
