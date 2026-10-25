import type { RevisionState } from "../types";

// A real minus sign, so a loss reads as clearly as a gain.
const wordDelta = (delta: number): string =>
  `${delta > 0 ? "+" : "−"}${Math.abs(delta)} ${Math.abs(delta) === 1 ? "word" : "words"}`;

const statusChange = (before: RevisionState, after: RevisionState): string => {
  if (before.status === after.status) return "";
  if (after.status === "archived") return "Archived";
  if (before.status === "archived") return "Restored";
  return after.status === "published" ? "Published" : "Unpublished";
};

// Says in plain words what the save that turned `before` into `after` changed.
export const changeSummary = (before: RevisionState, after: RevisionState): string => {
  const parts = [
    before.title !== after.title ? "Title changed" : "",
    before.slug !== after.slug ? `Renamed to ${after.slug}` : "",
    statusChange(before, after),
    before.coverDigest !== after.coverDigest ? "Cover changed" : "",
    before.descriptionDigest !== after.descriptionDigest ? "Description changed" : "",
    before.settingsDigest !== after.settingsDigest ? "Settings changed" : "",
    before.words !== after.words ? wordDelta(after.words - before.words) : "",
  ].filter(Boolean);
  return parts.length ? parts.join(", ") : "No visible change";
};
