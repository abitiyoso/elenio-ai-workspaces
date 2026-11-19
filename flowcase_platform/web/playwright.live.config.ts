import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./e2e-live",
  testIgnore: "architect-model.spec.ts",
  workers: 1,
  fullyParallel: false,
  timeout: 30000,
  use: {
    baseURL: "http://127.0.0.1:5173",
    viewport: { width: 1440, height: 900 },
    headless: true,
    trace: "retain-on-failure",
  },
  reporter: [
    ["list"],
    [
      "json",
      {
        outputFile: "../../docs/elenio-handoff/evidence/live-e2e-results.json",
      },
    ],
  ],
  outputDir: "test-results/live",
});
