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
  webServer: [
    {
      command: "uv run --frozen python ../scripts/e2e-server.py",
      env: { BURZBLOG_E2E_ROOT: fixtureRoot },
      url: "http://127.0.0.1:8000/api/session",
      reuseExistingServer: false,
    },
    {
      command: "pnpm start",
      env: { HOST: "127.0.0.1", PORT: "3000" },
      url: "http://127.0.0.1:3000",
      reuseExistingServer: false,
    },
  ],
});
