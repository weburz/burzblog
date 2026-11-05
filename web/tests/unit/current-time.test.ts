import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { effectScope } from "vue";
import { tickMs, useCurrentTime } from "../../app/composables/useCurrentTime";
import { editedLabel } from "../../app/utils/time";

const mount = () => {
  const scope = effectScope();
  const clock = scope.run(() => useCurrentTime())!;
  return { scope, now: clock.now };
};

describe("useCurrentTime", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-11-05T10:00:00Z"));
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("ticks forward with the clock", () => {
    const { scope, now } = mount();
    const start = now.value;
    vi.advanceTimersByTime(tickMs);
    expect(now.value).toBe(start + tickMs);
    scope.stop();
  });

  it("shares a single interval across consumers", () => {
    const a = mount();
    const b = mount();
    expect(vi.getTimerCount()).toBe(1);
    expect(a.now).toBe(b.now);
    a.scope.stop();
    expect(vi.getTimerCount()).toBe(1);
    b.scope.stop();
  });

  it("stops the timer once nothing uses it, and restarts on demand", () => {
    const a = mount();
    a.scope.stop();
    expect(vi.getTimerCount()).toBe(0);
    const b = mount();
    expect(vi.getTimerCount()).toBe(1);
    b.scope.stop();
  });

  it("refreshes when the tab becomes visible", () => {
    const { scope, now } = mount();
    vi.setSystemTime(Date.now() + 5 * 60_000);
    expect(now.value).toBeLessThan(Date.now());
    document.dispatchEvent(new Event("visibilitychange"));
    expect(now.value).toBe(Date.now());
    scope.stop();
  });

  it("moves a label from Just now to N min ago", () => {
    const { scope, now } = mount();
    const iso = new Date(now.value).toISOString();
    expect(editedLabel(iso, now.value)).toBe("Just now");
    vi.advanceTimersByTime(3 * 60_000);
    expect(editedLabel(iso, now.value)).toBe("3 min ago");
    scope.stop();
  });
});
