import { defineConfig } from "@playwright/test";

// Browser acceptance against an already running, DISPOSABLE deployment behind its
// HTTPS reverse proxy. Not part of `pnpm check`; see docs/containers.md.
// TLS chain validation is done by scripts/proxy-smoke.py with the real trust root;
// the browser accepts a private test CA so the same run works on test hosts.
export default defineConfig({
  testDir: "tests/deploy",
  workers: 1,
  retries: 0,
  timeout: 60000,
  use: {
    baseURL: process.env.BURZBLOG_DEPLOY_URL,
    ignoreHTTPSErrors: process.env.BURZBLOG_DEPLOY_PRIVATE_CA === "1",
    trace: "retain-on-failure",
  },
});
