import { describe, expect, it } from "vitest";
import { slugify } from "../../app/utils/posts";

describe("slugify", () => {
  it("lowercases and hyphenates", () => {
    expect(slugify("Hello, World! 2027")).toBe("hello-world-2027");
  });
  it("drops non-ASCII letters and trims", () => {
    expect(slugify("  café ‑ crème ")).toBe("caf-crme");
  });
  it("bounds long titles without a trailing hyphen", () => {
    const slug = slugify(`${"word ".repeat(40)}end`);
    expect(slug.length).toBeLessThanOrEqual(128);
    expect(slug.endsWith("-")).toBe(false);
    expect(slugify("a".repeat(127) + " b")).toBe("a".repeat(127));
  });
  it("collapses separators and handles empty results", () => {
    expect(slugify("a --  b")).toBe("a-b");
    expect(slugify("日本語")).toBe("");
  });
});
