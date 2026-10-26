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
    featured: false,
    publishedOn: null,
    updatedOn: null,
    ...change,
  });

const show = (
  draft: PostDraft,
  extra: { status?: "draft" | "published"; redirects?: string[] } = {},
) =>
  mount(PostDetails, {
    props: { open: true, draft, status: "draft", savedSlug: "hello", ...extra },
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
        USwitch: {
          props: ["modelValue", "id", "label"],
          emits: ["update:modelValue"],
          template:
            "<input type='checkbox' :id='id' :aria-label='label' :checked='modelValue' @change='$emit(\"update:modelValue\", $event.target.checked)' />",
        },
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

describe("post settings: slug redirects", () => {
  it("promises a redirect when a live post's slug changes", () => {
    const hint = (draft: PostDraft, status: "draft" | "published") =>
      show(draft, { status }).find("#detail-slug-hint").text();
    expect(hint(draftWith({ slug: "renamed" }), "published")).toContain(
      "The old address hello will redirect here.",
    );
    expect(hint(draftWith(), "published")).not.toContain("redirect");
    expect(hint(draftWith({ slug: "renamed" }), "draft")).not.toContain("redirect");
  });

  it("lists existing redirects and asks to remove one", async () => {
    const wrapper = show(draftWith(), { redirects: ["first", "second"] });
    const items = wrapper.findAll(".redirects li");
    expect(items.map((li) => li.find(".mono").text())).toEqual(["first", "second"]);
    await items[1]!.find("button").trigger("click");
    expect(wrapper.emitted("removeRedirect")).toEqual([["second"]]);
  });

  it("shows no list without redirects", () => {
    expect(show(draftWith()).find(".redirects").exists()).toBe(false);
    expect(show(draftWith(), { redirects: [] }).find(".redirects").exists()).toBe(false);
  });
});

describe("post settings: featured", () => {
  it("shows the flag and changes it on the draft", async () => {
    const draft = draftWith({ featured: false });
    const wrapper = show(draft);
    const toggle = wrapper.get("#detail-featured");
    expect((toggle.element as HTMLInputElement).checked).toBe(false);
    await toggle.setValue(true);
    expect(draft.featured).toBe(true);
    await toggle.setValue(false);
    expect(draft.featured).toBe(false);
  });
  it("shows a featured post as on", () => {
    const wrapper = show(draftWith({ featured: true }));
    expect((wrapper.get("#detail-featured").element as HTMLInputElement).checked).toBe(
      true,
    );
  });
});
