import { describe, expect, it } from "vitest";
import { editedLabel, exactTime } from "../../app/utils/time";

describe("editedLabel", () => {
  const now = Date.parse("2026-10-09T12:00:00Z");
  const ago = (ms: number) => new Date(now - ms).toISOString();
  it("counts recent changes in minutes, hours and days", () => {
    expect(editedLabel(ago(20_000), now)).toBe("Just now");
    expect(editedLabel(ago(5 * 60_000), now)).toBe("5 min ago");
    expect(editedLabel(ago(3 * 3_600_000), now)).toBe("3 h ago");
    expect(editedLabel(ago(30 * 3_600_000), now)).toBe("Yesterday");
    expect(editedLabel(ago(4 * 86_400_000), now)).toBe("4 days ago");
  });
  it("falls back to a date, with the year only when it differs", () => {
    expect(editedLabel(ago(30 * 86_400_000), now)).not.toMatch(/2026/);
    expect(editedLabel("2024-03-01T10:00:00Z", now)).toMatch(/2024/);
  });
  it("treats clock skew as just now and ignores invalid input", () => {
    expect(editedLabel(new Date(now + 60_000).toISOString(), now)).toBe("Just now");
    expect(editedLabel("not a date", now)).toBe("");
    expect(exactTime("not a date")).toBe("");
  });
});
