import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";
import PostsView from "../../app/components/PostsView.vue";
import StatusBadge from "../../app/components/StatusBadge.vue";
import { ApiError, request } from "../../app/utils/api";
import type { PostList } from "../../app/types";

vi.mock("../../app/utils/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../app/utils/api")>()),
  request: vi.fn<() => Promise<unknown>>(),
}));
afterEach(() => vi.clearAllMocks());

const mountView = async (list: PostList) => {
  vi.mocked(request).mockResolvedValue(list);
  const wrapper = mount(PostsView, {
    global: {
      components: { StatusBadge },
      stubs: {
        UButton: { template: "<button><slot /></button>" },
        UInput: {
          props: ["modelValue"],
          emits: ["update:modelValue"],
          template:
            "<input :value='modelValue' @input='$emit(\"update:modelValue\", $event.target.value)' />",
        },
        UModal: {
          template: "<div><slot /><slot name='body' /><slot name='footer' /></div>",
        },
        UIcon: true,
        USkeleton: true,
      },
    },
  });
  await flushPromises();
  return wrapper;
};

describe("posts list states", () => {
  it("distinguishes an empty content folder", async () => {
    const wrapper = await mountView({ items: [], total: 0 });
    expect(wrapper.text()).toContain("Nothing here yet");
    expect(wrapper.text()).not.toContain("No posts match.");
    wrapper.unmount();
  });

  it("says when the filter excludes everything", async () => {
    const wrapper = await mountView({
      items: [{ path: "posts/a.md", title: "Alpha", status: "draft" }],
      total: 1,
    });
    expect(wrapper.text()).toContain("Alpha");
    await wrapper.get("input").setValue("zzz");
    expect(wrapper.text()).toContain("No posts match.");
    expect(wrapper.text()).not.toContain("Nothing here yet");
    wrapper.unmount();
  });

  it("shows filenames in rows and a live match count while filtering", async () => {
    const wrapper = await mountView({
      items: [
        { path: "posts/a.md", title: "Alpha", status: "draft" },
        { path: "posts/b.md", title: "Beta", status: "published" },
      ],
      total: 2,
    });
    expect(wrapper.get(".post-path").text()).toBe("a.md");
    expect(wrapper.get('[role="status"]').text()).toBe("");
    await wrapper.get("input").setValue("alp");
    expect(wrapper.get('[role="status"]').text()).toBe("1 of 2 posts match");
    wrapper.unmount();
  });
  it("previews the path a new post will create", async () => {
    const wrapper = await mountView({ items: [], total: 0 });
    expect(wrapper.text()).toContain("Creates posts/….md");
    await wrapper.get("#new-title").setValue("Hello World");
    expect(wrapper.text()).toContain("Creates posts/hello-world.md");
    wrapper.unmount();
  });
});

describe("creating a post", () => {
  it("names the file that already exists instead of the generic conflict text", async () => {
    const wrapper = await mountView({ items: [], total: 0 });
    vi.mocked(request).mockRejectedValueOnce(
      new ApiError(409, "conflict", "Content or repository state changed."),
    );
    await wrapper.get("#new-title").setValue("Welcome");
    await wrapper.get("#new-post-form").trigger("submit");
    await flushPromises();
    const alert = wrapper.get('[role="alert"]').text();
    expect(alert).toContain(
      "posts/welcome.md already exists. Choose a different filename.",
    );
    expect(alert).not.toContain("Content or repository state changed");
    wrapper.unmount();
  });

  it("keeps the server message for other creation failures", async () => {
    const wrapper = await mountView({ items: [], total: 0 });
    vi.mocked(request).mockRejectedValueOnce(
      new ApiError(400, "invalid", "The operation cannot be performed safely."),
    );
    await wrapper.get("#new-title").setValue("Welcome");
    await wrapper.get("#new-post-form").trigger("submit");
    await flushPromises();
    const alert = wrapper.get('[role="alert"]').text();
    expect(alert).toContain("The operation cannot be performed safely.");
    expect(alert).not.toContain("already exists");
    wrapper.unmount();
  });
});
