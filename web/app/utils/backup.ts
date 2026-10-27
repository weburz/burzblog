// A full snapshot older than this is shown as overdue: a week of writing is
// the most a lost server should cost.
export const SNAPSHOT_STALE_MS = 7 * 24 * 3_600_000;

export type SnapshotState = "none" | "stale" | "ok";

// How the Backup section shows the newest snapshot the server recorded.
export const snapshotState = (
  snapshot: { at: string } | null | undefined,
  now = Date.now(),
): SnapshotState => {
  if (!snapshot) return "none";
  const at = Date.parse(snapshot.at);
  if (Number.isNaN(at) || now - at > SNAPSHOT_STALE_MS) return "stale";
  return "ok";
};
