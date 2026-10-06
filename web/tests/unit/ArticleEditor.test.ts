import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";
import ArticleEditor from "../../app/components/ArticleEditor.vue";
import ConfirmDialog from "../../app/components/ConfirmDialog.vue";
import StatusBadge from "../../app/components/StatusBadge.vue";
import { ApiError, request } from "../../app/utils/api";

vi.mock("../../app/utils/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../app/utils/api")>()),
  request: vi.fn<() => Promise<unknown>>(),
}));
afterEach(() => vi.clearAllMocks());

const global = {
  components: { StatusBadge, ConfirmDialog },
  stubs: {
    UButton: { template: "<button><slot /></button>" },
    UInput: true,
    UModal: true,
    UTooltip: { template: "<span><slot /></span>" },
    UKbd: true,
    UIcon: true,
  },
};

describe("editor ownership of unsent input", () => {
  it("checking an unchanged disk file never discards unsaved writing", async () => {
    const article = {
      path: "posts/a.md",
      revision: "a".repeat(64),
      markdown: "disk body\n",
      title: "A",
      status: "draft",
    };
    vi.mocked(request).mockResolvedValue(article);
    const wrapper = mount(ArticleEditor, {
      props: { article },
      global,
    });
    await wrapper.get("textarea").setValue("unsaved browser text");
    await wrapper
      .findAll("button")
      .find((button) => button.text() === "Check disk")
      ?.trigger("click");
    await flushPromises();
    expect(wrapper.get("textarea").element.value).toBe("unsaved browser text");
    expect(wrapper.text()).toContain("Unsaved changes");
    wrapper.unmount();
  });
  it("failed saves retain text and expose a persistent error", async () => {
    vi.mocked(request).mockRejectedValue(
      new Error("Conflict: external version retained"),
    );
    const wrapper = mount(ArticleEditor, {
      props: {
        article: {
          path: "a.md",
          revision: "a".repeat(64),
          markdown: "disk",
          title: "A",
          status: "draft",
        },
      },
      global,
    });
    await wrapper.get("textarea").setValue("keep my writing");
    await wrapper
      .findAll("button")
      .find((button) => button.text() === "Save")
      ?.trigger("click");
    await flushPromises();
    expect(wrapper.get("textarea").element.value).toBe("keep my writing");
    expect(wrapper.get('[role="alert"]').text()).toContain("Conflict");
    wrapper.unmount();
  });
  it("tracks saved state across preview", async () => {
    const article = {
      path: "posts/a.md",
      revision: "a".repeat(64),
      markdown: "disk body\n",
      title: "A",
      status: "draft",
    };
    const wrapper = mount(ArticleEditor, { props: { article }, global });
    const status = () => wrapper.get('[role="status"]').text();
    const press = (name: string) =>
      wrapper
        .findAll("button")
        .find((button) => button.text() === name)
        ?.trigger("click");
    expect(status()).toContain("On disk");
    await wrapper.get("textarea").setValue("new body\n");
    expect(status()).toContain("Unsaved changes");
    vi.mocked(request).mockResolvedValueOnce({
      ...article,
      revision: "b".repeat(64),
      markdown: "new body\n",
    });
    await press("Save");
    await flushPromises();
    expect(status()).toContain("Saved ✓");
    vi.mocked(request).mockResolvedValueOnce({ html: "<p>new body</p>" });
    await press("Preview");
    await flushPromises();
    expect(status()).toContain("Saved ✓");
    vi.mocked(request).mockResolvedValueOnce({
      ...article,
      revision: "c".repeat(64),
      markdown: "new body\n",
    });
    await press("Check disk");
    await flushPromises();
    expect(status()).toContain("On disk");
    wrapper.unmount();
  });
});

describe("a refused save", () => {
  const article = {
    path: "posts/a.md",
    revision: "a".repeat(64),
    markdown: "original body\n",
    title: "A",
    status: "draft",
  };
  // PUT is the save; the one-argument request is the read of the disk version.
  const refuseSave = async (
    refusal: ApiError,
    disk: () => Promise<unknown>,
    edit = true,
  ) => {
    vi.mocked(request).mockImplementation(async (_path, method) => {
      if (method === "PUT") throw refusal;
      return disk();
    });
    const errorHandler = vi.fn<(error: unknown) => void>();
    const wrapper = mount(ArticleEditor, {
      props: { article },
      global: { ...global, config: { errorHandler } },
    });
    if (edit) await wrapper.get("textarea").setValue("unsaved browser text");
    await wrapper
      .findAll("button")
      .find((button) => button.text() === "Save")
      ?.trigger("click");
    await flushPromises();
    return { wrapper, errorHandler };
  };
  it("loads the disk version when nothing in the editor was edited", async () => {
    const { wrapper, errorHandler } = await refuseSave(
      new ApiError(409, "conflict", "generic conflict"),
      async () => ({
        ...article,
        revision: "b".repeat(64),
        markdown: "disk body\nchanged on disk\n",
      }),
      false,
    );
    const page = wrapper.text();
    expect(page).not.toContain("This file changed outside BurzBlog.");
    expect(page).not.toContain("Save did not happen");
    expect(wrapper.findAll('[role="alert"]')).toHaveLength(0);
    expect(wrapper.get("textarea").element.value).toBe("disk body\nchanged on disk\n");
    expect(wrapper.get('[role="status"]').text()).toContain("On disk");
    expect(errorHandler).not.toHaveBeenCalled();
    wrapper.unmount();
  });
  it("compares with the disk version at once when the file changed", async () => {
    const { wrapper, errorHandler } = await refuseSave(
      new ApiError(409, "conflict", "generic conflict"),
      async () => ({
        ...article,
        revision: "b".repeat(64),
        markdown: "disk body\nchanged on disk\n",
      }),
    );
    const page = wrapper.text();
    expect(page).toContain("This file changed outside BurzBlog.");
    expect(page).toContain("changed on disk");
    expect(page).not.toContain("Save did not happen");
    expect(page).not.toContain("generic conflict");
    expect(wrapper.findAll('[role="alert"]')).toHaveLength(1);
    expect(wrapper.get("textarea").element.value).toBe("unsaved browser text");
    expect(wrapper.get('[role="status"]').text()).toContain("Unsaved changes");
    expect(request).toHaveBeenLastCalledWith("/api/post?path=posts%2Fa.md");
    expect(errorHandler).not.toHaveBeenCalled();
    wrapper.unmount();
  });
  it("keeps the error block when the disk revision is unchanged", async () => {
    const { wrapper, errorHandler } = await refuseSave(
      new ApiError(409, "conflict", "generic conflict"),
      async () => article,
    );
    const page = wrapper.text();
    expect(page).toContain("Save did not happen");
    expect(page).toContain("generic conflict");
    expect(page).not.toContain("This file changed outside BurzBlog.");
    expect(wrapper.get("textarea").element.value).toBe("unsaved browser text");
    expect(errorHandler).not.toHaveBeenCalled();
    wrapper.unmount();
  });
  it("keeps the error block and throws nothing when the disk read fails", async () => {
    const { wrapper, errorHandler } = await refuseSave(
      new ApiError(409, "conflict", "generic conflict"),
      () => Promise.reject(new ApiError(0, "network", "disk read failed")),
    );
    const page = wrapper.text();
    expect(page).toContain("Save did not happen");
    expect(page).toContain("generic conflict");
    expect(page).not.toContain("disk read failed");
    expect(page).not.toContain("This file changed outside BurzBlog.");
    expect(errorHandler).not.toHaveBeenCalled();
    wrapper.unmount();
  });
  it("does not read the disk after any other failure", async () => {
    const { wrapper, errorHandler } = await refuseSave(
      new ApiError(500, "storage", "disk full"),
      async () => ({ ...article, revision: "b".repeat(64) }),
    );
    const page = wrapper.text();
    expect(request).toHaveBeenCalledTimes(1);
    expect(request).toHaveBeenCalledWith("/api/post", "PUT", {
      path: "posts/a.md",
      revision: "a".repeat(64),
      markdown: "unsaved browser text",
    });
    expect(page).toContain("Save did not happen");
    expect(page).toContain("disk full");
    expect(page).not.toContain("This file changed outside BurzBlog.");
    expect(errorHandler).not.toHaveBeenCalled();
    wrapper.unmount();
  });
});
