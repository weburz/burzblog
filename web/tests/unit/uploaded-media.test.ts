import { flushPromises, mount } from "@vue/test-utils";
import { defineComponent, effectScope, h } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import MediaAltField from "../../app/components/MediaAltField.vue";
import { useUploadedMedia } from "../../app/composables/useUploadedMedia";
import type { MediaItem } from "../../app/types";

const request = vi.hoisted(() =>
  vi.fn<(path: string, method?: string, body?: unknown) => Promise<unknown>>(),
);
vi.mock("../../app/utils/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../app/utils/api")>()),
  request,
}));

const item = (change: Partial<MediaItem> = {}): MediaItem => ({
  id: 1,
  name: "a.png",
  url: "https://blog.example.com/media/a.png",
  originalName: "a.png",
  mime: "image/png",
  size: 10,
  alt: "",
  usedIn: 0,
  uploadedBy: "ada",
  createdAt: "2026-10-26T10:00:00Z",
  ...change,
});

beforeEach(() => {
  request.mockReset();
  vi.useFakeTimers();
});
afterEach(() => vi.useRealTimers());

const withMedia = () => effectScope().run(() => useUploadedMedia())!;

describe("useUploadedMedia", () => {
  it("sends the search text and kind to the server", async () => {
    request.mockResolvedValue({ items: [], total: 0 });
    const media = withMedia();
    media.query.value = "  beach ";
    media.kind.value = "image";
    await media.load();
    expect(request).toHaveBeenCalledWith(
      "/api/media?limit=48&offset=0&q=beach&type=image",
    );
  });

  it("waits while typing and asks once", async () => {
    request.mockResolvedValue({ items: [item()], total: 1 });
    const media = withMedia();
    media.query.value = "b";
    media.typeAhead();
    media.query.value = "be";
    media.typeAhead();
    await vi.advanceTimersByTimeAsync(100);
    expect(request).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(300);
    expect(request).toHaveBeenCalledTimes(1);
    expect(request.mock.calls[0]?.[0]).toContain("q=be");
    expect(media.items.value).toHaveLength(1);
  });

  it("ignores a slow answer to an earlier search", async () => {
    let slow: (value: unknown) => void = () => {};
    request.mockReturnValueOnce(new Promise((resolve) => (slow = resolve)));
    request.mockResolvedValueOnce({ items: [item({ id: 2 })], total: 1 });
    const media = withMedia();
    const first = media.load();
    await media.load();
    slow({ items: [item({ id: 1 })], total: 1 });
    await first;
    expect(media.items.value.map((i) => i.id)).toEqual([2]);
  });

  it("saves alt text and replaces the card", async () => {
    request.mockResolvedValue({ items: [item()], total: 1 });
    const media = withMedia();
    await media.load();
    request.mockResolvedValue(item({ alt: "A dot", usedIn: 2 }));
    expect(await media.saveAlt(item(), "A dot")).toBe("");
    expect(request).toHaveBeenLastCalledWith("/api/media/1", "PATCH", { alt: "A dot" });
    expect(media.items.value[0]?.alt).toBe("A dot");
    expect(media.items.value[0]?.usedIn).toBe(2);
  });

  it("reports why an alt text was not saved", async () => {
    request.mockRejectedValue(new Error("offline"));
    const media = withMedia();
    expect(await media.saveAlt(item(), "x")).not.toBe("");
  });
});

const UInput = defineComponent({
  inheritAttrs: false,
  props: ["modelValue"],
  emits: ["update:modelValue"],
  setup:
    (props, { attrs, emit }) =>
    () =>
      h("input", {
        ...attrs,
        value: props.modelValue,
        onInput: (e: Event) =>
          emit("update:modelValue", (e.target as HTMLInputElement).value),
      }),
});

const UButton = defineComponent({
  setup:
    (_, { attrs, slots }) =>
    () =>
      h("button", attrs, slots.default?.()),
});

const field = (save: (alt: string) => Promise<string>, alt = "Saved") =>
  mount(MediaAltField, {
    props: { inputId: "alt-1", alt, label: "a.png", save },
    global: { stubs: { UInput, UButton } },
  });
const saved = () => vi.fn<(alt: string) => Promise<string>>().mockResolvedValue("");

describe("MediaAltField", () => {
  it("saves a change on Enter and on blur, but only once", async () => {
    const save = saved();
    const wrapper = field(save);
    const input = wrapper.find("input");
    await input.setValue("New text ");
    await input.trigger("keydown", { key: "Enter" });
    await input.trigger("blur");
    expect(save.mock.calls).toEqual([["New text"]]);
    await flushPromises();
    await input.setValue("Other");
    await input.trigger("blur");
    expect(save).toHaveBeenCalledTimes(2);
  });

  it("does not save when nothing changed", async () => {
    const save = saved();
    const wrapper = field(save);
    const input = wrapper.find("input");
    await input.trigger("blur");
    await input.setValue("Saved");
    await input.trigger("blur");
    expect(save).not.toHaveBeenCalled();
  });

  it("puts the saved text back on Escape", async () => {
    const save = saved();
    const wrapper = field(save);
    const input = wrapper.find("input");
    await input.setValue("Typing");
    await input.trigger("keydown", { key: "Escape" });
    await input.trigger("blur");
    expect(save).not.toHaveBeenCalled();
    expect((input.element as HTMLInputElement).value).toBe("Saved");
  });

  it("keeps the typed text after a failed save and retries from the button", async () => {
    const save = vi
      .fn<(alt: string) => Promise<string>>()
      .mockResolvedValueOnce("Cannot reach BurzBlog.")
      .mockResolvedValueOnce("");
    const wrapper = field(save);
    const input = wrapper.find("input");
    await input.setValue("My description");
    await input.trigger("blur");
    await flushPromises();
    expect((input.element as HTMLInputElement).value).toBe("My description");
    expect(wrapper.get("[role=alert]").text()).toContain("Cannot reach BurzBlog.");
    // Leaving the field again does not send a second time by itself.
    await input.trigger("blur");
    expect(save).toHaveBeenCalledTimes(1);
    await wrapper.get("[role=alert] button").trigger("click");
    await flushPromises();
    expect(save.mock.calls).toEqual([["My description"], ["My description"]]);
    expect(wrapper.find("[role=alert]").exists()).toBe(false);
  });
});
