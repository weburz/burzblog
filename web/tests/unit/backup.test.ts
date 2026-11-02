import { describe, expect, it } from "vitest";
import { everyLabel, SNAPSHOT_STALE_MS, snapshotState } from "../../app/utils/backup";

describe("snapshotState", () => {
  const now = Date.parse("2026-10-27T12:00:00Z");
  const ago = (ms: number) => ({ at: new Date(now - ms).toISOString() });

  it("is none before the first snapshot", () => {
    expect(snapshotState(null, now)).toBe("none");
    expect(snapshotState(undefined, now)).toBe("none");
  });
  it("is ok for a snapshot within a week", () => {
    expect(snapshotState(ago(60_000), now)).toBe("ok");
    expect(snapshotState(ago(SNAPSHOT_STALE_MS), now)).toBe("ok");
  });
  it("is stale after a week, or when the time cannot be read", () => {
    expect(snapshotState(ago(SNAPSHOT_STALE_MS + 1), now)).toBe("stale");
    expect(snapshotState({ at: "not a time" }, now)).toBe("stale");
  });
});

describe("snapshotState with a schedule", () => {
  const now = Date.parse("2026-10-27T12:00:00Z");
  it("is failed when the last automatic snapshot failed, even after a good one", () => {
    expect(
      snapshotState({ at: new Date(now - 60_000).toISOString() }, now, {
        lastError: "AccessDenied",
      }),
    ).toBe("failed");
    expect(snapshotState(null, now, { lastError: "x" })).toBe("failed");
  });
  it("follows the newest snapshot while the schedule works", () => {
    expect(snapshotState({ at: new Date(now - 60_000).toISOString() }, now, {})).toBe(
      "ok",
    );
    expect(snapshotState(null, now, null)).toBe("none");
  });
});

describe("everyLabel", () => {
  it("names days, hours and minutes", () => {
    expect(everyLabel(86_400)).toBe("every day");
    expect(everyLabel(172_800)).toBe("every 2 days");
    expect(everyLabel(21_600)).toBe("every 6 h");
    expect(everyLabel(5_400)).toBe("every 90 min");
  });
});
