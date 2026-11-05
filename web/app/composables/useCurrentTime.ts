import { getCurrentScope, onScopeDispose, ref } from "vue";

export const tickMs = 30_000;

// The clock behind relative labels ("5 min ago"), shared by every consumer: one
// interval runs while anything uses it and stops when the last user goes away.
// It also catches up when the tab becomes visible again, as timers are throttled
// in the background.
const now = ref(Date.now());
let users = 0;
let timer: ReturnType<typeof setInterval> | undefined;

const refresh = () => {
  now.value = Date.now();
};

const onVisibility = () => {
  if (document.visibilityState === "visible") refresh();
};

const start = () => {
  if (users++ > 0) return;
  refresh();
  timer = setInterval(refresh, tickMs);
  document.addEventListener("visibilitychange", onVisibility);
};

const stop = () => {
  if (--users > 0) return;
  clearInterval(timer);
  timer = undefined;
  document.removeEventListener("visibilitychange", onVisibility);
};

export const useCurrentTime = () => {
  if (getCurrentScope()) {
    start();
    onScopeDispose(stop);
  }
  return { now };
};
