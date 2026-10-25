import { mount } from "@vue/test-utils";
import { defineComponent, h } from "vue";
import { describe, expect, it } from "vitest";
import PasswordInput from "../../app/components/PasswordInput.vue";

const UInput = defineComponent({
  inheritAttrs: false,
  props: ["modelValue"],
  emits: ["update:modelValue"],
  setup:
    (props, { attrs, slots, emit }) =>
    () =>
      h("span", [
        h("input", {
          ...attrs,
          value: props.modelValue,
          onInput: (e: Event) =>
            emit("update:modelValue", (e.target as HTMLInputElement).value),
        }),
        slots.trailing?.(),
      ]),
});
const UButton = defineComponent({
  inheritAttrs: false,
  setup:
    (_, { attrs }) =>
    () =>
      h("button", attrs),
});
const stubs = { UInput, UButton };

const make = () =>
  mount(PasswordInput, {
    props: {
      id: "pw",
      autocomplete: "current-password",
      modelValue: "hunter2hunter2",
      describedby: "pw-error",
    },
    attachTo: document.body,
    global: { stubs },
  });

const inputOf = (wrapper: ReturnType<typeof make>) =>
  wrapper.get("input").element as HTMLInputElement;

describe("password input", () => {
  it("toggles the type and aria state, keeping focus, caret and autocomplete", async () => {
    const wrapper = make();
    const input = inputOf(wrapper);
    const toggle = wrapper.get("#pw-toggle");
    expect(input.type).toBe("password");
    expect(toggle.attributes("aria-label")).toBe("Show password");
    expect(toggle.attributes("aria-pressed")).toBe("false");
    expect(toggle.attributes("aria-controls")).toBe("pw");

    input.focus();
    input.setSelectionRange(2, 5);
    await toggle.trigger("click");
    expect(input.type).toBe("text");
    expect(toggle.attributes("aria-label")).toBe("Hide password");
    expect(toggle.attributes("aria-pressed")).toBe("true");
    expect(document.activeElement).toBe(input);
    expect([input.selectionStart, input.selectionEnd]).toEqual([2, 5]);
    expect(input.getAttribute("autocomplete")).toBe("current-password");
    expect(input.getAttribute("aria-describedby")).toBe("pw-error pw-caps");

    await toggle.trigger("click");
    expect(input.type).toBe("password");
    wrapper.unmount();
  });

  it("hides the password again after its form is submitted", async () => {
    const form = document.createElement("form");
    document.body.append(form);
    const wrapper = mount(PasswordInput, {
      props: { id: "pw", autocomplete: "new-password" },
      attachTo: form,
      global: { stubs },
    });
    await wrapper.get("#pw-toggle").trigger("click");
    expect(inputOf(wrapper as never).type).toBe("text");
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    await wrapper.vm.$nextTick();
    expect(inputOf(wrapper as never).type).toBe("password");
    wrapper.unmount();
    form.remove();
  });

  it("shows the Caps Lock note only while Caps Lock is on", async () => {
    const wrapper = make();
    const note = wrapper.get("#pw-caps");
    expect(note.attributes("aria-live")).toBe("polite");
    expect(note.text()).toBe("");
    const input = inputOf(wrapper);
    const press = (caps: boolean) => {
      const event = new KeyboardEvent("keydown", { key: "a", bubbles: true });
      event.getModifierState = (key) => caps && key === "CapsLock";
      input.dispatchEvent(event);
    };
    press(true);
    await wrapper.vm.$nextTick();
    expect(note.text()).toBe("Caps Lock is on");
    press(false);
    await wrapper.vm.$nextTick();
    expect(note.text()).toBe("");
    press(true);
    await wrapper.vm.$nextTick();
    input.dispatchEvent(new FocusEvent("focusout", { bubbles: true }));
    await wrapper.vm.$nextTick();
    expect(note.text()).toBe("");
    wrapper.unmount();
  });
});
