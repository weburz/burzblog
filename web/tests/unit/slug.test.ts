import { describe, expect, it } from "vitest";
import {
  matchesTitle,
  slugify,
  slugPattern,
  untitledPattern,
} from "../../app/utils/slug";

describe("matchesTitle", () => {
  it.each([
    ["untitled", "", true],
    ["untitled-3", "  ", true],
    ["hello-world", "Hello, World!", true],
    ["hello-world-2", "Hello, World!", true],
    ["hello", "Hello, World!", false],
    ["hello-world-two", "Hello, World!", false],
    ["my-own-slug", "Hello, World!", false],
    ["untitled", "!!!", true],
    ["untitled", "Hello", false],
  ])("%s for title %j is %s", (slug, title, expected) => {
    expect(matchesTitle(slug, title)).toBe(expected);
  });
});

describe("untitledPattern", () => {
  it.each([
    ["untitled", true],
    ["untitled-12", true],
    ["untitled-", false],
    ["untitled-draft", false],
    ["my-untitled", false],
  ])("%s is %s", (slug, expected) => {
    expect(untitledPattern.test(slug)).toBe(expected);
  });
});

describe("slugify", () => {
  it.each([
    ["Hello, World!", "hello-world"],
    ["  Go 1.27 -- what's new ", "go-1-27-what-s-new"],
    ["Ünïcode café", "unicode-cafe"],
    ["!!!", ""],
  ])("%s → %s", (input, slug) => {
    expect(slugify(input)).toBe(slug);
    expect(slug === "" || slugPattern.test(slug)).toBe(true);
  });
  it("keeps one trailing hyphen while typing", () => {
    expect(slugify("my--", true)).toBe("my-");
    expect(slugify("my--")).toBe("my");
  });
  it("stays within 120 characters", () => {
    expect(slugify("word ".repeat(60)).length).toBeLessThanOrEqual(120);
  });
});
