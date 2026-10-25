import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import TotpStep from "../../app/components/TotpStep.vue";
import { groupSecret } from "../../app/utils/totp";

describe("setup key", () => {
  it("groups a secret in fours without a trailing space", () => {
    expect(groupSecret("ABCDEFGHIJ")).toBe("ABCD EFGH IJ");
    expect(groupSecret("ABCDEFGH")).toBe("ABCD EFGH");
    expect(groupSecret("")).toBe("");
  });
  it("is stable when the secret is already grouped", () => {
    expect(groupSecret("ABCD EFGH IJ")).toBe("ABCD EFGH IJ");
  });
});

const mountStep = (props: Record<string, unknown> = {}) =>
  mount(TotpStep, {
    props,
    global: {
      stubs: {
        UInput: {
          props: ["modelValue"],
          emits: ["update:modelValue"],
          template:
            "<input :value='modelValue' @input=\"$emit('update:modelValue', $event.target.value)\" />",
        },
        UButton: { template: "<button><slot /></button>" },
      },
    },
  });

describe("two-factor sign-in step", () => {
  it("submits the trimmed code", async () => {
    const wrapper = mountStep();
    await wrapper.find("input").setValue(" 123456 ");
    await wrapper.find("form").trigger("submit");
    expect(wrapper.emitted("submit")).toEqual([["123456"]]);
  });
  it("goes back to the password from Back, without submitting", async () => {
    const wrapper = mountStep();
    const back = wrapper.findAll("button").find((b) => b.text() === "Back");
    await back?.trigger("click");
    expect(wrapper.emitted("restart")).toHaveLength(1);
    expect(wrapper.emitted("submit")).toBeUndefined();
  });
  it("shows a wrong-code error as an alert", () => {
    const wrapper = mountStep({ error: "That code is not right." });
    expect(wrapper.find("[role=alert]").text()).toBe("That code is not right.");
    expect(mountStep().find("[role=alert]").exists()).toBe(false);
  });
});
