import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";
import ArticleEditor from "../../app/components/ArticleEditor.vue";
import { request } from "../../app/utils/api";

vi.mock("../../app/utils/api", () => ({
  request: vi.fn<() => Promise<unknown>>(),
  errorMessage: (error: Error) => error.message,
}));
afterEach(() => vi.clearAllMocks());

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
      global: {
        stubs: { UButton: { template: "<button><slot /></button>" }, UInput: true },
      },
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
      global: {
        stubs: { UButton: { template: "<button><slot /></button>" }, UInput: true },
      },
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
});
