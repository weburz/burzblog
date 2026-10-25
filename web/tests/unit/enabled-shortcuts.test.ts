import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { nextTick } from "vue";

// The switch is shared module state, so each test starts from a fresh module.
const load = async () => {
  vi.resetModules();
  return (await import("../../app/composables/useEnabledShortcuts")).useEnabledShortcuts;
};

beforeEach(() => localStorage.clear());
afterEach(() => vi.restoreAllMocks());

describe("useEnabledShortcuts", () => {
  it("is on by default", async () => {
    const useEnabledShortcuts = await load();
    expect(useEnabledShortcuts().enabled.value).toBe(true);
  });

  it("remembers the choice", async () => {
    const useEnabledShortcuts = await load();
    useEnabledShortcuts().enabled.value = false;
    await nextTick();
    expect(localStorage.getItem("burzblog:shortcuts")).toBe("0");
    useEnabledShortcuts().enabled.value = true;
    await nextTick();
    expect(localStorage.getItem("burzblog:shortcuts")).toBe("1");
  });

  it("restores a saved off choice", async () => {
    localStorage.setItem("burzblog:shortcuts", "0");
    const useEnabledShortcuts = await load();
    expect(useEnabledShortcuts().enabled.value).toBe(false);
  });

  it("shares one switch between callers", async () => {
    const useEnabledShortcuts = await load();
    const first = useEnabledShortcuts();
    const second = useEnabledShortcuts();
    first.enabled.value = false;
    expect(second.enabled.value).toBe(false);
  });

  it("works when storage is unavailable", async () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("denied");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("denied");
    });
    const useEnabledShortcuts = await load();
    const { enabled } = useEnabledShortcuts();
    expect(enabled.value).toBe(true);
    enabled.value = false;
    await nextTick();
    expect(enabled.value).toBe(false);
  });
});
