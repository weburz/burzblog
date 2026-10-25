import { describe, expect, it } from "vitest";
import {
  editedLabel,
  exactTime,
  fromLocalInput,
  isScheduled,
  MAX_SCHEDULE_MS,
  publishedLabel,
  publishesLabel,
  scheduledLabel,
  scheduleProblem,
  suggestedSchedule,
  toLocalInput,
  zoneLabel,
} from "../../app/utils/time";

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

describe("publishedLabel", () => {
  const now = Date.parse("2026-10-09T12:00:00Z");
  it("prefixes the date and adds the year only when it differs", () => {
    expect(publishedLabel("2026-07-03T12:00:00Z", now)).toMatch(/^Published .*3/);
    expect(publishedLabel("2026-07-03T12:00:00Z", now)).not.toMatch(/2026/);
    expect(publishedLabel("2024-07-03T12:00:00Z", now)).toMatch(/2024/);
  });
  it("ignores invalid input", () => {
    expect(publishedLabel("not a date", now)).toBe("");
  });
});

describe("datetime-local conversion", () => {
  it("round-trips through local input values", () => {
    const iso = "2026-04-25T01:46:00Z";
    expect(fromLocalInput(toLocalInput(iso))).toBe(iso);
  });
  it("keeps empty values empty", () => {
    expect(toLocalInput(null)).toBe("");
    expect(fromLocalInput("")).toBeNull();
    expect(toLocalInput("nonsense")).toBe("");
  });
});

describe("scheduling helpers", () => {
  const now = Date.parse("2026-10-25T12:00:00Z");
  const local = (ms: number) => toLocalInput(new Date(ms).toISOString());

  it("treats only drafts with a publish time as scheduled", () => {
    expect(isScheduled({ status: "draft", publishAt: "2026-11-03T09:00:00Z" })).toBe(
      true,
    );
    expect(isScheduled({ status: "draft", publishAt: null })).toBe(false);
    expect(isScheduled({ status: "draft" })).toBe(false);
    expect(isScheduled({ status: "published", publishAt: "2026-11-03T09:00:00Z" })).toBe(
      false,
    );
  });

  it("labels the time, with the year only when it differs", () => {
    const label = scheduledLabel("2026-11-03T09:00:00Z", now);
    expect(label).toMatch(/^Scheduled for /);
    expect(label).not.toMatch(/2026/);
    expect(scheduledLabel("2027-11-03T09:00:00Z", now)).toMatch(/2027/);
    expect(publishesLabel("2026-11-03T09:00:00Z", now)).toMatch(/^Publishes /);
    expect(scheduledLabel("not a date", now)).toBe("");
    expect(publishesLabel("not a date", now)).toBe("");
  });

  it("accepts a time in the next two years", () => {
    expect(scheduleProblem(local(now + 3_600_000), now)).toBe("");
    expect(scheduleProblem(local(now + MAX_SCHEDULE_MS - 3_600_000), now)).toBe("");
  });

  it("explains times that cannot be scheduled", () => {
    expect(scheduleProblem("", now)).toMatch(/date and time/);
    expect(scheduleProblem("not a date", now)).toMatch(/date and time/);
    expect(scheduleProblem(local(now - 3_600_000), now)).toMatch(/future/);
    expect(scheduleProblem(local(now + MAX_SCHEDULE_MS + 86_400_000), now)).toMatch(
      /2 years/,
    );
  });

  it("suggests tomorrow at nine, local", () => {
    const value = suggestedSchedule(now);
    expect(value).toMatch(/T09:00$/);
    expect(scheduleProblem(value, now)).toBe("");
  });

  it("names the time zone", () => {
    expect(zoneLabel(now)).not.toBe("");
  });
});
