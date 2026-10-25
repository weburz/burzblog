import { describe, expect, it } from "vitest";
import { passwordProblem, setupTokenFromHash } from "../../app/utils/setup";

describe("setup link", () => {
  it("reads the token from the address fragment", () => {
    expect(setupTokenFromHash("#token=abc_DEF-123")).toBe("abc_DEF-123");
    expect(setupTokenFromHash("#x=1&token=abc")).toBe("abc");
  });
  it("finds no token when there is none", () => {
    expect(setupTokenFromHash("")).toBe("");
    expect(setupTokenFromHash("#")).toBe("");
    expect(setupTokenFromHash("#other=1")).toBe("");
  });
});

describe("first password", () => {
  it("asks for at least 12 characters", () => {
    expect(passwordProblem("short", "short")).toMatch(/at least 12/);
    expect(passwordProblem("elevenchars", "elevenchars")).toMatch(/at least 12/);
    expect(passwordProblem("twelve chars", "twelve chars")).toBe("");
  });
  it("counts characters, not UTF-16 units", () => {
    expect(passwordProblem("😀".repeat(11), "😀".repeat(11))).toMatch(/at least 12/);
    expect(passwordProblem("😀".repeat(12), "😀".repeat(12))).toBe("");
  });
  it("needs the repeat to match", () => {
    expect(passwordProblem("twelve chars", "twelve charz")).toMatch(/different/);
  });
});
