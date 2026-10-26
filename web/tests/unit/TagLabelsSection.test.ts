import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import TagLabelsSection from "../../app/components/TagLabelsSection.vue";
import type { TagCount } from "../../app/types";

const request = vi.hoisted(() =>
  vi.fn<(path: string, method?: string, body?: unknown) => Promise<unknown>>(),
);
vi.mock("../../app/utils/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../app/utils/api")>()),
  request,
}));
const toast = vi.hoisted(() => ({ add: vi.fn<(t: unknown) => void>() }));
vi.stubGlobal("useToast", () => toast);

const tags: TagCount[] = [
  { tag: "constitutional-law", count: 2, label: "Constitutional Law", custom: false },
  { tag: "go", count: 1, label: "Golang", custom: true },
];

const show = async () => {
  request.mockResolvedValueOnce({ items: structuredClone(tags) });
  const wrapper = mount(TagLabelsSection, {
    global: {
      stubs: {
        UInput: {
          props: ["modelValue", "id"],
          emits: ["update:modelValue"],
          template:
            "<input :id='id' :value='modelValue' @input='$emit(\"update:modelValue\", $event.target.value)' />",
        },
        UButton: {
          props: ["disabled", "type"],
          template:
            "<button :type='type || \"button\"' :disabled='disabled'><slot /></button>",
        },
      },
    },
  });
  await flushPromises();
  return wrapper;
};

beforeEach(() => {
  request.mockReset();
  toast.add.mockReset();
});

describe("tag labels", () => {
  it("lists tags with the label readers see and labelled fields", async () => {
    const wrapper = await show();
    expect(request).toHaveBeenCalledWith("/api/tags");
    expect(wrapper.text()).toContain("constitutional-law");
    const field = wrapper.get<HTMLInputElement>("#tag-label-constitutional-law");
    expect(field.element.value).toBe("Constitutional Law");
    expect(wrapper.find("label[for='tag-label-go']").text()).toBe("Label for go");
  });

  it("saves only a changed, non-empty label", async () => {
    const wrapper = await show();
    const save = () => wrapper.findAll("button[type='submit']")[0]!;
    expect(save().attributes("disabled")).toBeDefined();
    await wrapper.get("#tag-label-constitutional-law").setValue("   ");
    expect(save().attributes("disabled")).toBeDefined();
    request.mockResolvedValueOnce({
      tag: "constitutional-law",
      label: "Verfassungsrecht",
      custom: true,
    });
    await wrapper.get("#tag-label-constitutional-law").setValue("  Verfassungsrecht ");
    expect(save().attributes("disabled")).toBeUndefined();
    await wrapper.findAll("form")[0]!.trigger("submit");
    await flushPromises();
    expect(request).toHaveBeenLastCalledWith(
      "/api/tags/constitutional-law/label",
      "PUT",
      { label: "  Verfassungsrecht " },
    );
    // The field shows what the server kept, trimmed.
    expect(
      wrapper.get<HTMLInputElement>("#tag-label-constitutional-law").element.value,
    ).toBe("Verfassungsrecht");
    expect(toast.add).toHaveBeenCalledWith(expect.objectContaining({ color: "success" }));
  });

  it("offers Reset only for custom labels and shows the derived label after it", async () => {
    const wrapper = await show();
    const reset = wrapper.findAll("button").filter((b) => b.text() === "Reset");
    expect(reset).toHaveLength(1);
    request.mockResolvedValueOnce({ tag: "go", label: "Go", custom: false });
    await reset[0]!.trigger("click");
    await flushPromises();
    expect(request).toHaveBeenLastCalledWith("/api/tags/go/label", "DELETE");
    expect(wrapper.get<HTMLInputElement>("#tag-label-go").element.value).toBe("Go");
    expect(wrapper.findAll("button").filter((b) => b.text() === "Reset")).toHaveLength(0);
  });

  it("reports a refused label and keeps the field", async () => {
    const wrapper = await show();
    request.mockRejectedValueOnce(new Error("Label is too long."));
    await wrapper.get("#tag-label-go").setValue("Gopher");
    await wrapper.findAll("form")[1]!.trigger("submit");
    await flushPromises();
    expect(toast.add).toHaveBeenCalledWith(expect.objectContaining({ color: "error" }));
    expect(wrapper.get<HTMLInputElement>("#tag-label-go").element.value).toBe("Gopher");
  });

  it("explains when there are no tags", async () => {
    request.mockResolvedValueOnce({ items: [] });
    const wrapper = mount(TagLabelsSection);
    await flushPromises();
    expect(wrapper.text()).toContain("No tags yet");
  });
});
