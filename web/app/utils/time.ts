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
