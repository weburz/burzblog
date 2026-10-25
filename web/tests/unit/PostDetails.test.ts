import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import { reactive } from "vue";
import PostDetails from "../../app/components/PostDetails.vue";
import type { PostDraft } from "../../app/types";

const draftWith = (change: Partial<PostDraft> = {}): PostDraft =>
  reactive({
    slug: "hello",
    title: "Hello",
    description: "",
    body: "",
    coverUrl: "",
    coverAlt: "",
    extra: "",
    tags: [],
    author: "ada",
    seoTitle: "",
    seoDescription: "",
    canonicalUrl: "",
    publishedOn: null,
    updatedOn: null,
    ...change,
  });

const show = (draft: PostDraft) =>
  mount(PostDetails, {
    props: { open: true, draft, status: "draft", savedSlug: "hello" },
    global: {
      stubs: {
        USlideover: { template: "<div><slot name='body' /></div>" },
        UInput: {
          props: ["modelValue"],
          emits: ["update:modelValue"],
          template:
            "<input :value='modelValue' @input='$emit(\"update:modelValue\", $event.target.value)' />",
        },
        UInputTags: { name: "UInputTags", props: ["modelValue"], template: "<input />" },
        UCollapsible: {
          props: ["open"],
          template:
            "<div :data-open='open'><slot /><slot v-if='open' name='content' /></div>",
        },
        UButton: { template: "<button><slot /></button>" },
      },
    },
  });

describe("post settings: blog metadata", () => {
  it("cleans tags as the writer adds them", async () => {
    const draft = draftWith();
    const wrapper = show(draft);
    wrapper
      .findComponent({ name: "UInputTags" })
      .vm.$emit("update:modelValue", ["Machine Learning", "go", "GO", "  "]);
    expect(draft.tags).toEqual(["machine-learning", "go"]);
  });

  it("edits the author", async () => {
    const draft = draftWith();
    const wrapper = show(draft);
    await wrapper.find("#detail-author").setValue("Grace Hopper");
    expect(draft.author).toBe("Grace Hopper");
  });

  it("keeps SEO closed until it has something to show", () => {
    expect(show(draftWith()).find("#detail-seo-title").exists()).toBe(false);
    for (const change of [
      { seoTitle: "A title" },
      { seoDescription: "A summary" },
      { canonicalUrl: "https://example.com/a" },
    ])
      expect(show(draftWith(change)).find("#detail-seo-title").exists()).toBe(true);
  });

  it("counts SEO characters and warns past the advised length", async () => {
    const wrapper = show(draftWith({ seoTitle: "Short" }));
    expect(wrapper.find("#detail-seo-title-hint").text()).toContain("5 / 60");
    expect(wrapper.find("#detail-seo-title-hint").classes()).not.toContain(
      "counter-over",
    );
    await wrapper.find("#detail-seo-title").setValue("x".repeat(70));
    expect(wrapper.find("#detail-seo-title-hint").text()).toContain("70 / 60");
    expect(wrapper.find("#detail-seo-title-hint").classes()).toContain("counter-over");
    expect(wrapper.find("#detail-seo-description-hint").text()).toContain("0 / 160");
  });

  it("explains a canonical URL that is not https", async () => {
    const wrapper = show(draftWith({ canonicalUrl: "http://example.com/a" }));
    expect(wrapper.find("#detail-canonical-hint").text()).toContain("full https address");
    await wrapper.find("#detail-canonical").setValue("https://example.com/a");
    expect(wrapper.find("#detail-canonical-hint").text()).not.toContain(
      "full https address",
    );
  });
});
