// One formatter per shape, created once: list rows render hundreds of these.
const sameYear = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" });
const otherYear = new Intl.DateTimeFormat(undefined, {
  year: "numeric",
  month: "short",
  day: "numeric",
});
const exact = new Intl.DateTimeFormat(undefined, {
  dateStyle: "medium",
  timeStyle: "short",
});

// "Just now", "5 min ago", "3 h ago", "Yesterday", "4 days ago", then a date.
export const editedLabel = (iso: string, now = Date.now()): string => {
  const then = Date.parse(iso);
  if (Number.isNaN(then)) return "";
  const minutes = Math.floor((now - then) / 60000);
  if (minutes < 1) return "Just now";
  if (minutes < 60) return `${minutes} min ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} h ago`;
  const days = Math.floor(hours / 24);
  if (days === 1) return "Yesterday";
  if (days < 7) return `${days} days ago`;
  const date = new Date(then);
  return (
    date.getFullYear() === new Date(now).getFullYear() ? sameYear : otherYear
  ).format(date);
};

// "Published 3 Oct", with the year only when it differs from now's.
export const publishedLabel = (iso: string, now = Date.now()): string => {
  const then = Date.parse(iso);
  if (Number.isNaN(then)) return "";
  const date = new Date(then);
  const format =
    date.getFullYear() === new Date(now).getFullYear() ? sameYear : otherYear;
  return `Published ${format.format(date)}`;
};

export const exactTime = (iso: string | null | undefined): string => {
  const then = iso ? Date.parse(iso) : Number.NaN;
  return Number.isNaN(then) ? "" : exact.format(then);
};

const pad = (n: number) => String(n).padStart(2, "0");

// ISO timestamp to the value a datetime-local input shows, in local time.
export const toLocalInput = (iso: string | null): string => {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
};

// A datetime-local value back to an ISO timestamp; empty stays empty.
export const fromLocalInput = (value: string): string | null => {
  if (!value) return null;
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? null : d.toISOString().replace(/\.\d{3}Z$/, "Z");
};

// A post is scheduled while it is a draft with a publish time.
export const isScheduled = (post: {
  status: string;
  publishAt?: string | null;
}): boolean => post.status === "draft" && Boolean(post.publishAt);

const scheduleSameYear = new Intl.DateTimeFormat(undefined, {
  day: "numeric",
  month: "short",
  hour: "2-digit",
  minute: "2-digit",
});
const scheduleOtherYear = new Intl.DateTimeFormat(undefined, {
  year: "numeric",
  day: "numeric",
  month: "short",
  hour: "2-digit",
  minute: "2-digit",
});
const zone = new Intl.DateTimeFormat(undefined, { timeZoneName: "short" });

// "3 Nov, 09:00", with the year only when it differs from now's.
const scheduleStamp = (iso: string, now: number): string => {
  const then = Date.parse(iso);
  if (Number.isNaN(then)) return "";
  const date = new Date(then);
  return (
    date.getFullYear() === new Date(now).getFullYear()
      ? scheduleSameYear
      : scheduleOtherYear
  ).format(date);
};

// "Scheduled for 3 Nov, 09:00".
export const scheduledLabel = (iso: string, now = Date.now()): string => {
  const stamp = scheduleStamp(iso, now);
  return stamp && `Scheduled for ${stamp}`;
};

// "Publishes 3 Nov, 09:00", the list row's counterpart of "Published 3 Oct".
export const publishesLabel = (iso: string, now = Date.now()): string => {
  const stamp = scheduleStamp(iso, now);
  return stamp && `Publishes ${stamp}`;
};

// The viewer's time zone as the schedule dialog shows it: "Asia/Kolkata (GMT+5:30)".
export const zoneLabel = (at = Date.now()): string => {
  const name = zone.formatToParts(at).find((part) => part.type === "timeZoneName")?.value;
  const id = Intl.DateTimeFormat().resolvedOptions().timeZone;
  return name && id && id !== name ? `${id} (${name})` : (id ?? name ?? "");
};

// The server accepts publish times up to two years ahead.
export const MAX_SCHEDULE_MS = 2 * 365 * 24 * 3_600_000;

// Why a datetime-local value cannot be a publish time, or "" when it can.
export const scheduleProblem = (value: string, now = Date.now()): string => {
  const iso = fromLocalInput(value);
  if (!iso) return "Pick a date and time.";
  const at = Date.parse(iso);
  if (at <= now) return "Pick a time in the future.";
  if (at > now + MAX_SCHEDULE_MS) return "Pick a time within the next 2 years.";
  return "";
};

// Tomorrow at 09:00 local, as a datetime-local value: a sensible first pick.
export const suggestedSchedule = (now = Date.now()): string => {
  const d = new Date(now);
  d.setDate(d.getDate() + 1);
  d.setHours(9, 0, 0, 0);
  return toLocalInput(d.toISOString());
};
