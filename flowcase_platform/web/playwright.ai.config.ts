import { defineConfig } from "@playwright/test";
import live from "./playwright.live.config";
export default defineConfig({
  ...live,
  testMatch: "architect-model.spec.ts",
  testIgnore: [],
  timeout: 180000,
  reporter: [
    ["list"],
    [
      "json",
      {
        outputFile:
          "../../docs/elenio-handoff/evidence/architect-ai-results.json",
      },
    ],
  ],
  outputDir: "test-results/architect-ai",
});
