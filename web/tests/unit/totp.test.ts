import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import TotpStep from "../../app/components/TotpStep.vue";
import { groupSecret, qrPath } from "../../app/utils/totp";

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

describe("setup QR code", () => {
  const uri =
    "otpauth://totp/BurzBlog:ada?secret=BO4A3WN7HCW3L7G3QHMP6XT7WE53JYHD&issuer=BurzBlog";
  const dark = (d: string, x: number, y: number) => d.includes(`M${x} ${y}h1v1h-1z`);

  it("is a square of 21 + 4n modules plus the quiet zone", () => {
    const { size } = qrPath(uri, 2);
    expect((size - 4 - 21) % 4).toBe(0);
    expect(size).toBeGreaterThan(25);
  });
  it("leaves the quiet zone light and draws the finder patterns", () => {
    const { d, size } = qrPath(uri, 2);
    for (let i = 0; i < size; i++) {
      expect(dark(d, i, 0) || dark(d, 0, i) || dark(d, i, 1) || dark(d, 1, i)).toBe(
        false,
      );
    }
    // Top-left finder: dark outer ring, light inner ring, dark centre.
    expect(dark(d, 2, 2)).toBe(true);
    expect(dark(d, 8, 8)).toBe(true);
    expect(dark(d, 3, 3)).toBe(false);
    expect(dark(d, 5, 5)).toBe(true);
    // Top-right and bottom-left finders.
    expect(dark(d, size - 3, 2)).toBe(true);
    expect(dark(d, 2, size - 3)).toBe(true);
  });
  it("depends on the text", () => {
    expect(qrPath(uri).d).toBe(qrPath(uri).d);
    expect(qrPath(uri).d).not.toBe(qrPath(uri.replace("ada", "grace")).d);
  });
});
