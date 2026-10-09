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

export const exactTime = (iso: string): string => {
  const then = Date.parse(iso);
  return Number.isNaN(then) ? "" : exact.format(then);
};
