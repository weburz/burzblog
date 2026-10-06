import { readdirSync } from "node:fs";

// These packages are build/dev tools, not part of the supported deployment.
const paths = readdirSync(".output", { recursive: true });
const forbidden = paths.filter((path) =>
  /(^|[/\\])(node-forge|listhen|esbuild|simple-git|braces|@nuxt[/\\]devtools)([/\\]|$)/.test(
    path,
  ),
);
if (forbidden.length) {
  throw new Error(
    "Build-only TLS/bundler/devtools packages appeared in the deployable output.",
  );
}
console.log(
  "Runtime artifact boundary: no node-forge, listhen, esbuild, simple-git, braces or @nuxt/devtools packages.",
);
