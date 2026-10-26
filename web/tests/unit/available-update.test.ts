import { beforeEach, describe, expect, it, vi } from "vitest";

const request = vi.hoisted(() => vi.fn<(path: string) => Promise<unknown>>());
vi.mock("../../app/utils/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../app/utils/api")>()),
  request,
}));

// The state is shared module state, so each test starts from a fresh module.
const load = async () => {
  vi.resetModules();
  return (await import("../../app/composables/useAvailableUpdate")).useAvailableUpdate;
};

const info = (latest: string, available = true) => ({
  current: "1.1.0",
  latest,
  available,
  url: `https://github.com/weburz/burzblog/releases/tag/v${latest}`,
  checkedAt: "2026-10-26T10:00:00Z",
});

beforeEach(() => {
  localStorage.clear();
  request.mockReset();
  vi.restoreAllMocks();
});

describe("useAvailableUpdate", () => {
  it("shows an available update", async () => {
    request.mockResolvedValue({ enabled: true, update: info("1.2.0") });
    const api = (await load())();
    await api.load();
    expect(request).toHaveBeenCalledWith("/api/updates");
    expect(api.enabled.value).toBe(true);
    expect(api.visible.value?.latest).toBe("1.2.0");
  });

  it("shows nothing when checks are off or nothing is newer", async () => {
    const api = (await load())();
    request.mockResolvedValue({ enabled: false, update: null });
    await api.load();
    expect(api.enabled.value).toBe(false);
    expect(api.visible.value).toBeNull();
    request.mockResolvedValue({ enabled: true, update: info("1.1.0", false) });
    await api.load();
    expect(api.visible.value).toBeNull();
  });

  it("dismisses per version and remembers it", async () => {
    request.mockResolvedValue({ enabled: true, update: info("1.2.0") });
    const api = (await load())();
    await api.load();
    api.dismiss();
    expect(api.visible.value).toBeNull();
    expect(localStorage.getItem("burzblog:update-dismissed")).toBe("1.2.0");

    // A reload keeps it dismissed.
    const again = (await load())();
    await again.load();
    expect(again.visible.value).toBeNull();

    // A newer release shows the notice again.
    request.mockResolvedValue({ enabled: true, update: info("1.3.0") });
    await again.load();
    expect(again.visible.value?.latest).toBe("1.3.0");
  });

  it("still works when storage is unavailable", async () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("denied");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("denied");
    });
    request.mockResolvedValue({ enabled: true, update: info("1.2.0") });
    const api = (await load())();
    await api.load();
    expect(api.visible.value?.latest).toBe("1.2.0");
    api.dismiss();
    expect(api.visible.value).toBeNull();
  });

  it("treats a failed request as nothing to show", async () => {
    request.mockRejectedValue(new Error("offline"));
    const api = (await load())();
    await api.load();
    expect(api.enabled.value).toBe(false);
    expect(api.visible.value).toBeNull();
  });
});
