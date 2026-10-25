import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import SetupNotice from "../../app/components/SetupNotice.vue";
import { ApiError } from "../../app/utils/api";

const request = vi.hoisted(() => vi.fn<(path: string) => Promise<unknown>>());
vi.mock("../../app/utils/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../app/utils/api")>()),
  request,
}));

beforeEach(() => {
  request.mockReset();
});

describe("setup notice", () => {
  it("explains how to create the first account when none exists", async () => {
    request.mockResolvedValue({ needed: true });
    const wrapper = mount(SetupNotice);
    await flushPromises();
    expect(request).toHaveBeenCalledWith("/api/setup");
    expect(wrapper.text()).toContain("No accounts yet");
    expect(wrapper.text()).toContain("burzblog serve");
    expect(wrapper.text()).toContain("burzblog user add <name>");
  });
  it("says nothing once accounts exist", async () => {
    request.mockResolvedValue({ needed: false });
    const wrapper = mount(SetupNotice);
    await flushPromises();
    expect(wrapper.text()).toBe("");
  });
  it("says nothing when the server cannot be asked", async () => {
    request.mockRejectedValue(new ApiError(0, "network", "offline"));
    const wrapper = mount(SetupNotice);
    await flushPromises();
    expect(wrapper.text()).toBe("");
  });
});
