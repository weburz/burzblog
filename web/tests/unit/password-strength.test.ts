import { describe, expect, it } from "vitest";
import { passwordStrength } from "../../app/utils/password-strength";

describe("password strength", () => {
  it("says nothing for an empty password", () => {
    expect(passwordStrength("")).toMatchObject({ level: "empty", length: 0 });
  });

  it("counts characters, not UTF-16 units, and reports what is missing", () => {
    expect(passwordStrength("😀😀😀")).toMatchObject({ level: "short", length: 3 });
    expect(passwordStrength("elevenchars").hint).toContain("1 more");
  });

  it("flags repeats even when long enough", () => {
    for (const value of [
      "aaaaaaaaaaaa",
      "abababababab",
      "123123123123",
      "xKq9xKq9xKq9",
      "passsssword12",
    ]) {
      expect(`${value}:${passwordStrength(value).level}`).toBe(`${value}:weak`);
    }
  });

  it("rates length thresholds at 12 and 16", () => {
    expect(passwordStrength("twelve chars!").level).toBe("fair");
    expect(passwordStrength("0123456789ab").level).toBe("fair");
    expect(passwordStrength("0123456789abcdef").level).toBe("strong");
    expect(passwordStrength("correct horse battery").level).toBe("strong");
  });
});
