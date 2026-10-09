import { describe, expect, it } from "vitest";
import { pathProblem, slugify } from "../../app/utils/posts";

describe("slugify", () => {
  it("lowercases and hyphenates", () => {
    expect(slugify("Hello, World! 2027")).toBe("hello-world-2027");
  });
  it("removes accents, drops other non-ASCII characters and trims", () => {
    expect(slugify("  café ‑ crème ")).toBe("cafe-creme");
    expect(slugify("Naïve résumé ☕")).toBe("naive-resume");
    expect(slugify("Ångström Ⅻ")).toBe("angstrom-xii");
  });
  it("bounds long titles without a trailing hyphen", () => {
    const slug = slugify(`${"word ".repeat(40)}end`);
    expect(slug.length).toBeLessThanOrEqual(125);
    expect(slug.endsWith("-")).toBe(false);
    expect(slugify("a".repeat(124) + " b")).toBe("a".repeat(124));
  });
  it("collapses separators and handles empty results", () => {
    expect(slugify("a --  b")).toBe("a-b");
    expect(slugify("日本語")).toBe("");
  });
});

describe("pathProblem", () => {
  it("accepts ordinary content paths", () => {
    expect(pathProblem("posts/my-post.md")).toBe("");
    expect(pathProblem("notes/2026/a_b.c.md")).toBe("");
  });
  it("explains each refusal the server would make", () => {
    expect(pathProblem("")).toContain("Enter the new path");
    expect(pathProblem("posts/my-post")).toBe("End the filename with .md.");
    for (const path of [
      "../outside.md",
      "/etc/x.md",
      "posts//a.md",
      "posts/Bad Name.md",
      "posts/.hidden.md",
      "posts/-a.md",
    ])
      expect(pathProblem(path)).toContain("inside the content folder");
    expect(pathProblem(`${"a/".repeat(32)}b.md`)).toBe("Use at most 32 folder levels.");
  });
});
