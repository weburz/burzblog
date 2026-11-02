// A full snapshot older than this is shown as overdue: a week of writing is
// the most a lost server should cost.
export const SNAPSHOT_STALE_MS = 7 * 24 * 3_600_000;

export type SnapshotState = "none" | "stale" | "failed" | "ok";

// How the Backup section shows the newest snapshot the server recorded. A
// failed automatic snapshot outranks an older one that worked.
export const snapshotState = (
  snapshot: { at: string } | null | undefined,
  now = Date.now(),
  schedule?: { lastError?: string } | null,
): SnapshotState => {
  if (schedule?.lastError) return "failed";
  if (!snapshot) return "none";
  const at = Date.parse(snapshot.at);
  if (Number.isNaN(at) || now - at > SNAPSHOT_STALE_MS) return "stale";
  return "ok";
};

// "every day", "every 2 days", "every 6 h", "every 90 min".
export const everyLabel = (seconds: number): string => {
  const days = seconds / 86_400;
  if (Number.isInteger(days)) return days === 1 ? "every day" : `every ${days} days`;
  const hours = seconds / 3_600;
  if (Number.isInteger(hours)) return `every ${hours} h`;
  return `every ${Math.round(seconds / 60)} min`;
};
