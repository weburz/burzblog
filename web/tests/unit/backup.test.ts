import { describe, expect, it } from "vitest";
import { SNAPSHOT_STALE_MS, snapshotState } from "../../app/utils/backup";

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
