import {defineConfig} from "@playwright/test";
export default defineConfig({
  testDir:"./e2e-workspace", workers:1, fullyParallel:false, timeout:45000,
  use:{baseURL:"http://127.0.0.1:8200",viewport:{width:1440,height:900},headless:true,trace:"off"},
  // Auth cookies and OIDC exchanges must not be attached to committed evidence.
  reporter:[["list"],["json",{outputFile:"../../docs/elenio-handoff/evidence/workspace-e2e-results.json"}]],
  outputDir:"test-results/workspace",
});
