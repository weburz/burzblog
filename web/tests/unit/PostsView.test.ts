import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import PostsView from "../../app/components/PostsView.vue";
import PostRow from "../../app/components/PostRow.vue";
import NewPostDialog from "../../app/components/NewPostDialog.vue";
import StatusBadge from "../../app/components/StatusBadge.vue";
import { ApiError, request } from "../../app/utils/api";
import type { PostList, PostSummary } from "../../app/types";
import { defaultList, type ListState } from "../../app/utils/route";

vi.mock("../../app/utils/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../app/utils/api")>()),
  request: vi.fn<() => Promise<unknown>>(),
}));
beforeEach(() => vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] }));
afterEach(() => {
  vi.clearAllMocks();
  vi.useRealTimers();
});

const summary = (path: string, title: string, status = "draft"): PostSummary => ({
  path,
  title,
  status,
  modified: new Date(Date.now() - 5 * 60000).toISOString(),
});
const listOf = (items: PostSummary[], files = items.length): PostList => ({
  items,
  total: items.length,
  files,
});
const mountView = async (list: PostList, initial: Partial<ListState> = {}) => {
  vi.mocked(request).mockResolvedValue(list);
  const wrapper = mount(PostsView, {
    props: { initial: { ...defaultList, ...initial } },
    global: {
      components: { StatusBadge, PostRow, NewPostDialog },
      stubs: {
        UButton: { template: "<button><slot /></button>" },
        UInput: {
          props: ["modelValue"],
          emits: ["update:modelValue"],
          template:
            "<input :value='modelValue' @input='$emit(\"update:modelValue\", $event.target.value)' />",
        },
        USelect: {
          props: ["modelValue", "items"],
          emits: ["update:modelValue"],
          template:
            "<select :value='modelValue' @change='$emit(\"update:modelValue\", $event.target.value)'><option v-for='item in items' :key='item.value' :value='item.value'>{{ item.label }}</option></select>",
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
const lastQuery = () =>
  new URLSearchParams(String(vi.mocked(request).mock.lastCall?.[0]).split("?")[1]);

describe("posts list states", () => {
  it("distinguishes an empty content folder and hides the list controls", async () => {
    const wrapper = await mountView(listOf([]));
    expect(wrapper.text()).toContain("Nothing here yet");
    expect(wrapper.text()).not.toContain("No posts match.");
    expect(wrapper.find("[aria-label='Filter by status']").exists()).toBe(false);
    wrapper.unmount();
  });

  it("asks the server to search every file, not just the loaded page", async () => {
    const wrapper = await mountView(listOf([summary("posts/a.md", "Alpha")], 150));
    expect(lastQuery().get("sort")).toBe("modified");
    vi.mocked(request).mockResolvedValue({ items: [], total: 0, files: 150 });
    await wrapper.get("input").setValue("zzz");
    vi.advanceTimersByTime(300);
    await flushPromises();
    expect(lastQuery().get("q")).toBe("zzz");
    expect(lastQuery().get("offset")).toBe("0");
    expect(wrapper.text()).toContain("No posts match.");
    expect(wrapper.get("[role='status']").text()).toBe("0 of 150 files match");
    wrapper.unmount();
  });

  it("filters by status and sorts on the server", async () => {
    const wrapper = await mountView(listOf([summary("posts/a.md", "Alpha")]));
    await wrapper
      .findAll("button")
      .find((button) => button.text() === "Drafts")!
      .trigger("click");
    await flushPromises();
    expect(lastQuery().get("status")).toBe("draft");
    await wrapper.get("select").setValue("path");
    await flushPromises();
    expect(lastQuery().get("sort")).toBe("path");
    expect(lastQuery().get("status")).toBe("draft");
    wrapper.unmount();
  });

  it("shows only the newest answer when searches overlap", async () => {
    const wrapper = await mountView(listOf([summary("posts/a.md", "Alpha")]));
    let slow: (value: unknown) => void = () => {};
    vi.mocked(request)
      .mockReturnValueOnce(new Promise((resolve) => (slow = resolve)))
      .mockResolvedValueOnce(listOf([summary("posts/b.md", "Beta")], 2));
    await wrapper.get("input").setValue("a");
    vi.advanceTimersByTime(300);
    await wrapper.get("input").setValue("be");
    vi.advanceTimersByTime(300);
    await flushPromises();
    slow(listOf([summary("posts/a.md", "Stale")], 2));
    await flushPromises();
    expect(wrapper.text()).toContain("Beta");
    expect(wrapper.text()).not.toContain("Stale");
    wrapper.unmount();
  });

  it("shows filenames, untitled files and when each file changed", async () => {
    const wrapper = await mountView(
      listOf([summary("posts/a.md", "Alpha"), summary("notes/b.md", "")]),
    );
    const rows = wrapper.findAll(".post-row");
    expect(rows[0]!.get(".post-path").text()).toBe("a.md");
    expect(rows[1]!.get(".post-path").text()).toBe("notes/b.md");
    expect(rows[1]!.get(".post-title").text()).toBe("Untitled");
    expect(rows[0]!.get("time").text()).toBe("5 min ago");
    expect(wrapper.get("[role='status']").text()).toBe("2 files");
    wrapper.unmount();
  });
});

describe("creating a post", () => {
  const submit = async (
    wrapper: Awaited<ReturnType<typeof mountView>>,
    title: string,
  ) => {
    await wrapper.get("#new-title").setValue(title);
    await wrapper.get("#new-post-form").trigger("submit");
    await flushPromises();
  };
  it("previews the path a new post will create", async () => {
    const wrapper = await mountView(listOf([]));
    expect(wrapper.text()).toContain("Creates posts/….md");
    await wrapper.get("#new-title").setValue("Hello World");
    expect(wrapper.text()).toContain("Creates posts/hello-world.md");
    wrapper.unmount();
  });
  it("names the file that already exists instead of the generic text", async () => {
    const wrapper = await mountView(listOf([]));
    vi.mocked(request).mockRejectedValueOnce(
      new ApiError(409, "exists", "A file already exists at that path."),
    );
    await submit(wrapper, "Welcome");
    const alert = wrapper.get('[role="alert"]').text();
    expect(alert).toContain(
      "posts/welcome.md already exists. Choose a different filename.",
    );
    expect(alert).not.toContain("A file already exists at that path");
    wrapper.unmount();
  });
  it("shows the server's specific cause when it names one", async () => {
    const wrapper = await mountView(listOf([]));
    vi.mocked(request).mockRejectedValueOnce(
      new ApiError(
        400,
        "invalid",
        "The operation cannot be performed safely.",
        "slug must contain lowercase words separated by hyphens.",
      ),
    );
    await submit(wrapper, "Welcome");
    expect(wrapper.get('[role="alert"]').text()).toBe(
      "Slug must contain lowercase words separated by hyphens.",
    );
    wrapper.unmount();
  });
  it("refuses a blank title without asking the server", async () => {
    const wrapper = await mountView(listOf([]));
    vi.mocked(request).mockClear();
    await wrapper.get("#new-slug").setValue("blank");
    await submit(wrapper, "   ");
    expect(wrapper.get('[role="alert"]').text()).toBe("Give the post a title.");
    expect(request).not.toHaveBeenCalled();
    wrapper.unmount();
  });
});

describe("opening a post", () => {
  const two = listOf([summary("posts/a.md", "Alpha"), summary("posts/b.md", "Beta")]);
  it("reports a failed open on that row, not at the top of the list", async () => {
    const wrapper = await mountView(two);
    vi.mocked(request).mockRejectedValueOnce(
      new ApiError(404, "not_found", "That file or folder is not on disk."),
    );
    await wrapper.findAll(".post-row")[1]!.trigger("click");
    await flushPromises();
    const rows = wrapper.findAll("li");
    expect(rows[0]!.find('[role="alert"]').exists()).toBe(false);
    expect(rows[1]!.get('[role="alert"]').text()).toContain(
      "This file is no longer on disk.",
    );
    expect(wrapper.find(".alert").exists()).toBe(false);
    wrapper.unmount();
  });
  it("opens a post once however often the row is clicked", async () => {
    const wrapper = await mountView(two);
    let resolve: (value: unknown) => void = () => {};
    vi.mocked(request).mockClear();
    vi.mocked(request).mockReturnValueOnce(new Promise((done) => (resolve = done)));
    const row = wrapper.findAll(".post-row")[0]!;
    await row.trigger("click");
    await row.trigger("click");
    expect(request).toHaveBeenCalledTimes(1);
    expect(row.attributes("aria-busy")).toBe("true");
    resolve({
      path: "posts/a.md",
      revision: "a".repeat(64),
      markdown: "",
      title: "Alpha",
      status: "draft",
    });
    await flushPromises();
    expect(wrapper.emitted("open")).toHaveLength(1);
    wrapper.unmount();
  });
});

describe("the list and the address", () => {
  it("starts from the state the address describes", async () => {
    await mountView(listOf([]), {
      filter: "published",
      q: "notes",
      sort: "path",
      offset: 100,
    });
    const query = lastQuery();
    expect([
      query.get("status"),
      query.get("q"),
      query.get("sort"),
      query.get("offset"),
    ]).toEqual(["published", "notes", "path", "100"]);
  });
  it("reports what it shows so the address can follow", async () => {
    const wrapper = await mountView(listOf([summary("posts/a.md", "Alpha")]));
    expect(wrapper.emitted("change")?.at(-1)).toEqual([defaultList]);
    await wrapper
      .findAll("button")
      .find((button) => button.text() === "Drafts")!
      .trigger("click");
    await flushPromises();
    expect(wrapper.emitted("change")?.at(-1)).toEqual([
      { ...defaultList, filter: "draft" },
    ]);
    wrapper.unmount();
  });
});
