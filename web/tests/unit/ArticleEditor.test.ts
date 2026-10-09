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

describe("failures beside their controls", () => {
  const article = {
    path: "posts/a.md",
    revision: "a".repeat(64),
    markdown: "disk body\n",
    title: "A",
    status: "draft",
  };
  const withInputs = {
    ...global,
    stubs: {
      ...global.stubs,
      UInput: {
        props: ["modelValue"],
        emits: ["update:modelValue"],
        template:
          "<input :value='modelValue' @input='$emit(\"update:modelValue\", $event.target.value)' />",
      },
    },
  };
  const press = (wrapper: ReturnType<typeof mount>, name: string) =>
    wrapper
      .findAll("button")
      .find((button) => button.text() === name)
      ?.trigger("click");

  it("shows a refused save in the sticky save bar", async () => {
    vi.mocked(request).mockRejectedValue(
      new ApiError(0, "network", "Cannot reach BurzBlog."),
    );
    const wrapper = mount(ArticleEditor, { props: { article }, global });
    await wrapper.get("textarea").setValue("unsaved");
    await press(wrapper, "Save");
    await flushPromises();
    const alert = wrapper.get(".editor-footer [role='alert']");
    expect(alert.text()).toContain("Save did not happen");
    expect(alert.text()).toContain("Cannot reach BurzBlog.");
    expect(wrapper.findAll("[role='alert']")).toHaveLength(1);
    wrapper.unmount();
  });

  it("names an existing destination inside File options", async () => {
    vi.mocked(request).mockRejectedValue(
      new ApiError(409, "exists", "A file already exists at that path."),
    );
    const wrapper = mount(ArticleEditor, { props: { article }, global: withInputs });
    await wrapper.get("#destination").setValue("posts/b.md");
    await wrapper.get(".file-options form").trigger("submit");
    await flushPromises();
    const alert = wrapper.get(".file-options [role='alert']");
    expect(alert.text()).toContain("The file was not moved");
    expect(alert.text()).toContain("posts/b.md already exists. Nothing was overwritten");
    expect(wrapper.findAll("[role='alert']")).toHaveLength(1);
    await wrapper.get("#destination").setValue("posts/c.md");
    expect(wrapper.find(".file-options [role='alert']").exists()).toBe(false);
    wrapper.unmount();
  });

  it("explains a missing folder instead of a filesystem failure", async () => {
    vi.mocked(request).mockRejectedValue(
      new ApiError(404, "not_found", "That file or folder is not on disk."),
    );
    const wrapper = mount(ArticleEditor, { props: { article }, global: withInputs });
    await wrapper.get("#destination").setValue("drafts/new.md");
    await wrapper.get(".file-options form").trigger("submit");
    await flushPromises();
    expect(wrapper.get(".file-options [role='alert']").text()).toContain(
      "The folder drafts/ does not exist",
    );
    wrapper.unmount();
  });

  it("refuses an unsupported path before sending it", async () => {
    const wrapper = mount(ArticleEditor, { props: { article }, global: withInputs });
    await wrapper.get("#destination").setValue("../outside.md");
    expect(wrapper.text()).toContain("Use a path inside the content folder");
    const move = wrapper
      .findAll("button")
      .find((button) => button.text() === "Move file");
    expect(move?.attributes("disabled")).toBeDefined();
    await wrapper.get(".file-options form").trigger("submit");
    await flushPromises();
    expect(request).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("clears an expired-session failure once the session is restored", async () => {
    vi.mocked(request).mockRejectedValue(
      new ApiError(
        401,
        "authentication",
        "Your session expired. Sign in again, then retry.",
      ),
    );
    const wrapper = mount(ArticleEditor, { props: { article }, global });
    await wrapper.get("textarea").setValue("unsaved");
    await press(wrapper, "Save");
    await flushPromises();
    expect(wrapper.get(".editor-footer").text()).toContain("Your text is still here.");
    window.dispatchEvent(new CustomEvent("burzblog:session-restored"));
    await flushPromises();
    expect(wrapper.find("[role='alert']").exists()).toBe(false);
    expect(wrapper.get("textarea").element.value).toBe("unsaved");
    wrapper.unmount();
  });
});

describe("repairing a file whose frontmatter cannot be read", () => {
  const broken = {
    path: "posts/broken.md",
    revision: "a".repeat(64),
    markdown: "---\ntitle: [unterminated\n---\nBody\n",
    title: "",
    status: "unavailable",
    problem: "Malformed or duplicate-key YAML frontmatter.",
  };
  it("shows the exact text and the reason, and offers no Publish", () => {
    const wrapper = mount(ArticleEditor, { props: { article: broken }, global });
    expect(wrapper.get("textarea").element.value).toBe(broken.markdown);
    expect(wrapper.text()).toContain("BurzBlog cannot read this file's frontmatter");
    expect(wrapper.text()).toContain("Malformed or duplicate-key YAML frontmatter.");
    expect(wrapper.text()).toContain("Needs attention");
    expect(wrapper.findAll("button").some((button) => button.text() === "Publish")).toBe(
      false,
    );
    wrapper.unmount();
  });
  it("clears the warning once a valid version is saved", async () => {
    vi.mocked(request).mockResolvedValueOnce({
      ...broken,
      revision: "b".repeat(64),
      markdown: "---\ntitle: Fixed\n---\nBody\n",
      title: "Fixed",
      status: "draft",
      problem: null,
    });
    const wrapper = mount(ArticleEditor, { props: { article: broken }, global });
    await wrapper.get("textarea").setValue("---\ntitle: Fixed\n---\nBody\n");
    await wrapper
      .findAll("button")
      .find((button) => button.text() === "Save")
      ?.trigger("click");
    await flushPromises();
    expect(wrapper.text()).not.toContain("cannot read this file's frontmatter");
    expect(wrapper.get("h1").text()).toBe("Fixed");
    expect(wrapper.get('[role="status"]').text()).toContain("Saved ✓");
    wrapper.unmount();
  });
});

describe("publishing both ways", () => {
  it("offers Unpublish for a published post and returns it to draft", async () => {
    const published = {
      path: "posts/a.md",
      revision: "a".repeat(64),
      markdown: "---\nstatus: published\n---\nBody\n",
      title: "A",
      status: "published",
    };
    vi.mocked(request).mockResolvedValueOnce({
      ...published,
      revision: "b".repeat(64),
      markdown: "---\nstatus: draft\n---\nBody\n",
      status: "draft",
    });
    const wrapper = mount(ArticleEditor, {
      props: { article: published },
      global: {
        ...global,
        stubs: {
          ...global.stubs,
          UModal: { template: "<div><slot name='footer' /></div>" },
        },
      },
    });
    const buttons = () => wrapper.findAll("button").map((button) => button.text());
    expect(buttons()).toContain("Unpublish");
    expect(buttons()).not.toContain("Publish");
    const confirms = wrapper
      .findAll("button")
      .filter((button) => button.text() === "Unpublish");
    await confirms.at(-1)!.trigger("click");
    await flushPromises();
    expect(request).toHaveBeenCalledWith("/api/unpublish", "POST", {
      path: "posts/a.md",
      revision: "a".repeat(64),
    });
    expect(buttons()).toContain("Publish");
    wrapper.unmount();
  });
});
