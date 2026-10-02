import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";
import RepositoryPanel from "../../app/components/RepositoryPanel.vue";
import { ApiError, request } from "../../app/utils/api";
import type { GitState } from "../../app/types";

vi.mock("../../app/utils/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../app/utils/api")>()),
  request: vi.fn<() => Promise<unknown>>(),
}));
afterEach(() => vi.clearAllMocks());

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
      stubs: { UButton: { template: "<button><slot /></button>" }, UInput: true },
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
});
