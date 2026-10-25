import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { defineConfig } from "@playwright/test";

// E2E_PORT lets E2E run while a dev server holds :3000; `task check` keeps 3000.
const port = process.env.E2E_PORT ?? "3000";
const origin = `http://127.0.0.1:${port}`;
const fixtureRoot = mkdtempSync(join(tmpdir(), "burzblog-e2e-"));
export default defineConfig({
  testDir: "tests/e2e",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 45000,
  metadata: { fixtureRoot },
  use: { baseURL: origin, trace: "retain-on-failure" },
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
    env: { BURZBLOG_E2E_ROOT: fixtureRoot, E2E_PORT: port },
    url: `${origin}/healthz`,
    timeout: 180000,
    // Never adopt a server that is already there: a busy port fails the run.
    reuseExistingServer: false,
  },
});
