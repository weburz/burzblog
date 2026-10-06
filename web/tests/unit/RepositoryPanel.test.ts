import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import RepositoryPanel from "../../app/components/RepositoryPanel.vue";
import { ApiError, request } from "../../app/utils/api";
import type { GitState } from "../../app/types";

vi.mock("../../app/utils/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../app/utils/api")>()),
  request: vi.fn<() => Promise<unknown>>(),
}));
beforeEach(() => vi.stubGlobal("useToast", () => ({ add: vi.fn<() => void>() })));
afterEach(() => {
  vi.resetAllMocks();
  vi.unstubAllGlobals();
});

const state = (remote: GitState["remote"]): GitState => ({
  remote_configured: remote !== null,
  remote,
  repository: {
    branch: "main",
    detached: false,
    clean: true,
    branches: ["main"],
    changed_markdown: [],
    changes: [],
    upstream: null,
    ahead: null,
    behind: null,
    operation: null,
  },
});

const mountPanel = () =>
  mount(RepositoryPanel, {
    global: {
      stubs: {
        UButton: { template: "<button><slot /></button>" },
        UInput: true,
        UIcon: true,
      },
    },
  });

describe("repository remote presentation", () => {
  it("describes an authenticated HTTPS remote without exposing its address", async () => {
    vi.mocked(request).mockResolvedValue(
      state({ name: "origin", transport: "https", authenticated: true }),
    );
    const wrapper = mountPanel();
    await flushPromises();
    expect(wrapper.text()).toContain("origin · HTTPS with operator credentials");
    wrapper.unmount();
  });

  it("explains how operators configure a missing remote", async () => {
    vi.mocked(request).mockResolvedValue(state(null));
    const wrapper = mountPanel();
    await flushPromises();
    expect(wrapper.text()).toContain("BURZBLOG_REMOTE_URL");
    wrapper.unmount();
  });

  it("separates unreachable remotes from history conflicts", async () => {
    const configured = state({ name: "origin", transport: "https", authenticated: true });
    vi.mocked(request)
      .mockResolvedValueOnce(configured)
      .mockRejectedValueOnce(
        new ApiError(502, "remote", "The configured Git remote could not be reached."),
      )
      .mockResolvedValueOnce(configured);
    const wrapper = mountPanel();
    await flushPromises();
    await wrapper
      .findAll("button")
      .find((button) => button.text() === "Check for updates")
      ?.trigger("click");
    await flushPromises();
    const alert = wrapper.get("[role=alert]").text();
    expect(alert).toContain("Remote unavailable");
    expect(alert).not.toContain("Review both histories");
    wrapper.unmount();
  });

  it("counts changed Markdown files in the heading", async () => {
    const value = state({ name: "origin", transport: "file", authenticated: false });
    value.repository!.clean = false;
    value.repository!.changed_markdown = ["posts/a.md", "posts/b.md", "posts/c.md"];
    vi.mocked(request).mockResolvedValue(value);
    const wrapper = mountPanel();
    await flushPromises();
    expect(wrapper.get("h2").text()).toBe("3 Markdown files changed");
    wrapper.unmount();
  });

  it("makes Apply safe update primary only when behind and clean", async () => {
    const value = state({ name: "origin", transport: "file", authenticated: false });
    value.repository!.behind = 2;
    value.repository!.ahead = 0;
    vi.mocked(request).mockResolvedValue(value);
    const wrapper = mountPanel();
    await flushPromises();
    const apply = () =>
      wrapper.findAll("button").find((button) => button.text() === "Apply safe update");
    expect(apply()?.attributes("data-primary")).toBe("true");
    value.repository!.behind = 0;
    wrapper.unmount();
    const other = mountPanel();
    await flushPromises();
    expect(
      other
        .findAll("button")
        .find((button) => button.text() === "Apply safe update")
        ?.attributes("data-primary"),
    ).toBe("false");
    other.unmount();
  });

  it("explains a rejected commit and keeps the selection", async () => {
    const value = state({ name: "origin", transport: "file", authenticated: false });
    value.repository!.clean = false;
    value.repository!.changed_markdown = ["posts/a.md", "posts/b.md"];
    vi.mocked(request)
      .mockResolvedValueOnce(value)
      .mockRejectedValueOnce(new ApiError(400, "invalid", "Could not parse posts/a.md."))
      .mockResolvedValueOnce(value);
    const wrapper = mountPanel();
    await flushPromises();
    await wrapper.get('input[value="posts/a.md"]').setValue(true);
    await wrapper.get("form").trigger("submit");
    await flushPromises();
    const alert = wrapper.get("[role=alert]").text();
    expect(alert).toContain("That did not work");
    expect(alert).toContain("cannot be committed until they are fixed on disk");
    expect(alert).not.toContain("Nothing was merged");
    expect(
      (wrapper.get('input[value="posts/a.md"]').element as HTMLInputElement).checked,
    ).toBe(true);
    expect(
      (wrapper.get('input[value="posts/b.md"]').element as HTMLInputElement).checked,
    ).toBe(false);
    wrapper.unmount();
  });

  it("explains diverged histories and emphasises neither sync action", async () => {
    const value = state({ name: "origin", transport: "file", authenticated: false });
    value.repository!.upstream = "origin/main";
    value.repository!.ahead = 1;
    value.repository!.behind = 2;
    vi.mocked(request).mockResolvedValue(value);
    const wrapper = mountPanel();
    await flushPromises();
    expect(wrapper.text()).toContain(
      "Both this branch and origin/main have new commits. Apply safe update and Push commits will both be refused until the histories are reconciled with ordinary Git.",
    );
    const apply = wrapper
      .findAll("button")
      .find((button) => button.text() === "Apply safe update");
    expect(apply?.attributes("data-primary")).toBe("false");
    wrapper.unmount();
  });
});

describe("repository sync failures", () => {
  const press = (wrapper: ReturnType<typeof mountPanel>, label: string) =>
    wrapper
      .findAll("button")
      .find((button) => button.text() === label)
      ?.trigger("click");
  const configured = () =>
    state({ name: "origin", transport: "https", authenticated: true });
  const failPush = async (error: ApiError) => {
    vi.mocked(request)
      .mockResolvedValueOnce(configured())
      .mockRejectedValueOnce(error)
      .mockResolvedValueOnce(configured());
    const wrapper = mountPanel();
    await flushPromises();
    await press(wrapper, "Push commits");
    await flushPromises();
    return wrapper;
  };

  it("says nothing local changed when a push fails without a history conflict", async () => {
    const wrapper = await failPush(
      new ApiError(400, "invalid", "The operation cannot be performed safely."),
    );
    const alert = wrapper.get("[role=alert]").text();
    expect(alert).toContain("Nothing local was changed");
    expect(alert).not.toContain("Nothing was merged automatically");
    wrapper.unmount();
  });

  it("keeps the history advice for a push the remote refused as a conflict", async () => {
    const wrapper = await failPush(
      new ApiError(409, "conflict", "The remote refused this push."),
    );
    const alert = wrapper.get("[role=alert]").text();
    expect(alert).toContain("Nothing was merged automatically");
    expect(alert).not.toContain("Nothing local was changed");
    wrapper.unmount();
  });

  it("clears a stale sync error when the writer refreshes", async () => {
    const wrapper = await failPush(
      new ApiError(409, "conflict", "The remote refused this push."),
    );
    expect(wrapper.find("[role=alert]").exists()).toBe(true);
    vi.mocked(request).mockResolvedValueOnce(configured());
    await wrapper.get('button[aria-label="Refresh"]').trigger("click");
    await flushPromises();
    expect(wrapper.find("[role=alert]").exists()).toBe(false);
    wrapper.unmount();
  });

  it("explains that Apply safe update needs a clean working tree", async () => {
    const value = state({ name: "origin", transport: "file", authenticated: false });
    value.repository!.clean = false;
    value.repository!.changed_markdown = ["posts/a.md"];
    vi.mocked(request).mockResolvedValue(value);
    const wrapper = mountPanel();
    await flushPromises();
    const row = wrapper
      .findAll("li")
      .find((item) => item.text().includes("Apply safe update"));
    expect(row?.text()).toContain("Needs a clean working tree");
    expect(row?.text()).not.toContain("Fast-forwards");
    wrapper.unmount();
  });
});
