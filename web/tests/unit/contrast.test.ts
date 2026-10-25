import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const css = readFileSync("app/assets/main.css", "utf8");

// Declarations of one block of main.css.
const block = (selector: string) => {
  const start = css.indexOf(`\n${selector} {`);
  const body = css.slice(start, css.indexOf("\n}", start));
  return Object.fromEntries(
    [...body.matchAll(/(--[\w-]+):\s*([^;]+);/g)].map((m) => [m[1], m[2]!.trim()]),
  ) as Record<string, string>;
};
// .dark overrides sit on top of the light tokens.
const light = block(":root");
const dark = { ...light, ...block(".dark") };

// Resolves `var(--x)` chains to the final #rrggbb.
const resolve = (tokens: Record<string, string>, name: string): string => {
  const value = tokens[name]!;
  const ref = /^var\((--[\w-]+)\)$/.exec(value);
  return ref ? resolve(tokens, ref[1]!) : value;
};

const channel = (value: number) => {
  const c = value / 255;
  return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
};
const luminance = (hex: string) => {
  const n = parseInt(hex.slice(1), 16);
  return (
    0.2126 * channel(n >> 16) +
    0.7152 * channel((n >> 8) & 255) +
    0.0722 * channel(n & 255)
  );
};
// WCAG 2.x contrast ratio between two #rrggbb colors.
const contrast = (a: string, b: string) => {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi! + 0.05) / (lo! + 0.05);
};

describe("contrast", () => {
  it("matches the WCAG reference values", () => {
    expect(contrast("#000000", "#ffffff")).toBeCloseTo(21, 5);
    expect(contrast("#777777", "#ffffff")).toBeCloseTo(4.48, 2);
  });
});

const surfaces = ["--bb-bg", "--bb-surface", "--bb-surface-2"];

describe.each([
  ["light", light],
  ["dark", dark],
])("%s tokens", (_name, tokens) => {
  const color = (name: string) => resolve(tokens, name);

  it.each(["--bb-placeholder", "--ui-text-dimmed", "--bb-text-3"])(
    "%s is readable text (4.5:1) on every surface",
    (name) => {
      for (const surface of surfaces)
        expect(contrast(color(name), color(surface))).toBeGreaterThanOrEqual(4.5);
    },
  );

  it("--ui-border-accented outlines controls at 3:1 on every surface", () => {
    for (const surface of surfaces)
      expect(
        contrast(color("--ui-border-accented"), color(surface)),
      ).toBeGreaterThanOrEqual(3);
  });

  it("the selected-segment marker stands out at 3:1", () => {
    expect(contrast(color("--bb-blue"), color("--bb-surface"))).toBeGreaterThanOrEqual(3);
  });
});
