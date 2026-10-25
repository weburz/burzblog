import { describe, expect, it } from "vitest";
import { slugify, slugPattern } from "../../app/utils/slug";

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
