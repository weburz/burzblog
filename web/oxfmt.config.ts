import { defineConfig } from "oxfmt";

export default defineConfig({
  ignorePatterns: [
    ".nuxt",
    ".output",
    "node_modules",
    "pnpm-lock.yaml",
    "playwright-report",
    "test-results",
  ],
  printWidth: 90,
  semi: true,
  singleQuote: false,
  tabWidth: 2,
});
