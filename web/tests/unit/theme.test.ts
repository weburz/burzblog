import { describe, expect, it } from "vitest";
import {
  asThemePreference,
  isDark,
  themeBootScript,
  themeKey,
} from "../../app/utils/theme";

describe("asThemePreference", () => {
  it("keeps light and dark", () => {
    expect(asThemePreference("light")).toBe("light");
    expect(asThemePreference("dark")).toBe("dark");
  });
  it("falls back to system for anything else", () => {
    expect(asThemePreference(null)).toBe("system");
    expect(asThemePreference("purple")).toBe("system");
  });
});

describe("isDark", () => {
  it("follows the system only when asked to", () => {
    expect(isDark("system", true)).toBe(true);
    expect(isDark("system", false)).toBe(false);
    expect(isDark("light", true)).toBe(false);
    expect(isDark("dark", false)).toBe(true);
  });
});

describe("themeBootScript", () => {
  it("reads the same key the panel writes", () => {
    expect(themeBootScript).toContain(themeKey);
    expect(() => new Function(themeBootScript)).not.toThrow();
  });
});
