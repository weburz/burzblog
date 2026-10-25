import { defineNuxtConfig } from "nuxt/config";
import { themeBootScript } from "./app/utils/theme";

export default defineNuxtConfig({
  compatibilityDate: "2026-10-02",
  ssr: false,
  telemetry: false,
  devtools: { enabled: false },
  modules: ["@nuxt/ui"],
  css: ["~/assets/main.css"],
  ui: { fonts: false, colorMode: false },
  icon: {
    provider: "none",
    fallbackToApi: false,
    clientBundle: {
      scan: true,
      icons: [
        "lucide:loader-circle",
        "lucide:chevron-down",
        "lucide:x",
        "lucide:check",
        "lucide:circle-alert",
        "lucide:circle-check",
        "lucide:info",
        "lucide:chevron-right",
        "lucide:monitor",
        "lucide:sun",
        "lucide:moon",
        "lucide:arrow-down-up",
        "lucide:triangle-alert",
      ],
    },
  },
  app: {
    head: {
      title: "BurzBlog",
      htmlAttrs: { lang: "en" },
      meta: [
        { name: "robots", content: "noindex" },
        { name: "color-scheme", content: "light dark" },
      ],
      script: [{ innerHTML: themeBootScript, tagPosition: "head" }],
    },
  },
  routeRules: {
    "/**": {
      headers: {
        "X-Robots-Tag": "noindex",
        "X-Content-Type-Options": "nosniff",
        "Referrer-Policy": "no-referrer",
        "X-Frame-Options": "DENY",
      },
    },
    "/api/**": { proxy: "http://127.0.0.1:8080/api/**" },
  },
});
