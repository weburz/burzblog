import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import ConfirmDialog from "../../app/components/ConfirmDialog.vue";

describe("confirm dialog", () => {
  it("emits confirm from the confirm button only", async () => {
    const wrapper = mount(ConfirmDialog, {
      props: {
        open: true,
        title: "Leave without saving?",
        body: "Unsaved writing will be lost.",
        confirmLabel: "Leave",
        confirmColor: "error",
      },
      global: {
        stubs: {
          UModal: { template: "<div><slot /><slot name='footer' /></div>" },
          UButton: { template: "<button><slot /></button>" },
        },
      },
    });
    const buttons = wrapper.findAll("button");
    await buttons.find((button) => button.text() === "Cancel")?.trigger("click");
    expect(wrapper.emitted("confirm")).toBeUndefined();
    await buttons.find((button) => button.text() === "Leave")?.trigger("click");
    expect(wrapper.emitted("confirm")).toHaveLength(1);
  });
});
