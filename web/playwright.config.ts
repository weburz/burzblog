import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { defineConfig } from "@playwright/test";

const fixtureRoot = mkdtempSync(join(tmpdir(), "burzblog-e2e-"));
export default defineConfig({
  testDir: "tests/e2e",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 45000,
  metadata: { fixtureRoot },
  use: { baseURL: "http://127.0.0.1:3000", trace: "retain-on-failure" },
  projects: [
    { name: "desktop", use: { viewport: { width: 1365, height: 900 } } },
    {
      name: "tablet",
      testMatch: "editor.spec.ts",
      use: { viewport: { width: 820, height: 1180 } },
    },
    {
      name: "mobile",
      testMatch: "editor.spec.ts",
      use: { viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true },
    },
  ],
  // The production topology: one process serves the generated shell and the API.
  webServer: {
    command: "../scripts/e2e-server.sh",
    env: { BURZBLOG_E2E_ROOT: fixtureRoot },
    url: "http://127.0.0.1:3000/healthz",
    timeout: 180000,
    reuseExistingServer: false,
  },
});
