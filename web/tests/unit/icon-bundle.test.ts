import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

// The icon bundler scans templates, not plain .ts files, so icons named in
// utils and composables must be listed in nuxt.config.ts or they render blank.
const root = join(__dirname, "../..");
const sources = ["app/utils", "app/composables"].flatMap((dir) =>
  readdirSync(join(root, dir))
    .filter((name) => name.endsWith(".ts"))
    .map((name) => readFileSync(join(root, dir, name), "utf8")),
);
const config = readFileSync(join(root, "nuxt.config.ts"), "utf8");

describe("icon bundle", () => {
  it("lists every icon referenced outside templates", () => {
    const used = new Set(sources.flatMap((s) => s.match(/i-lucide-[a-z0-9-]+/g) ?? []));
    const missing = [...used].filter(
      (icon) => !config.includes(`"lucide:${icon.slice("i-lucide-".length)}"`),
    );
    expect(missing).toEqual([]);
  });
});
