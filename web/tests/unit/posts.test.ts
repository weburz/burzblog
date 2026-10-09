import { describe, expect, it } from "vitest";
import { pathProblem, slugify } from "../../app/utils/posts";

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
