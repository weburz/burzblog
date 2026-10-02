import { readdirSync } from "node:fs";

// These packages are build/dev tools, not part of the supported deployment.
const paths = readdirSync(".output", { recursive: true });
const forbidden = paths.filter((path) =>
  /(^|[/\\])(node-forge|listhen|esbuild)([/\\]|$)/.test(path),
);
if (forbidden.length) {
  throw new Error("Build-only TLS/bundler packages appeared in the deployable output.");
}
console.log("Runtime artifact boundary: no node-forge, listhen or esbuild packages.");
