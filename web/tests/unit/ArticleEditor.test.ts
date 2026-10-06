import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";
import ArticleEditor from "../../app/components/ArticleEditor.vue";
import ConfirmDialog from "../../app/components/ConfirmDialog.vue";
import StatusBadge from "../../app/components/StatusBadge.vue";
import { request } from "../../app/utils/api";

vi.mock("../../app/utils/api", () => ({
  request: vi.fn<() => Promise<unknown>>(),
  errorMessage: (error: Error) => error.message,
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
    wrapper.unmount();
  });
});
