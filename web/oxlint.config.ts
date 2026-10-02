import { defineConfig } from "oxlint";

export default defineConfig({
  plugins: ["typescript", "vue", "vitest"],
  categories: { correctness: "error", suspicious: "error" },
  env: { browser: true, node: true },
  globals: {
    defineAppConfig: "readonly",
    defineProps: "readonly",
    defineEmits: "readonly",
    defineModel: "readonly",
  },
  ignorePatterns: [
    ".nuxt",
    ".output",
    "node_modules",
    "playwright-report",
    "test-results",
  ],
});
