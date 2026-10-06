import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import StatusBadge from "../../app/components/StatusBadge.vue";

describe("status badge", () => {
  it.each([
    ["draft", "Draft"],
    ["published", "Published"],
    ["unavailable", "Needs attention"],
  ] as const)("renders %s as %s", (status, text) => {
    const wrapper = mount(StatusBadge, {
      props: { status },
      global: { stubs: { UIcon: true } },
    });
    expect(wrapper.text()).toBe(text);
  });

  it("only draws the bordered pill when asked, or for needs attention", () => {
    const stubs = { UIcon: true };
    const plain = mount(StatusBadge, {
      props: { status: "published" },
      global: { stubs },
    });
    expect(plain.classes()).not.toContain("badge-pill");
    const pill = mount(StatusBadge, {
      props: { status: "published", pill: true },
      global: { stubs },
    });
    expect(pill.classes()).toContain("badge-pill");
    const attention = mount(StatusBadge, {
      props: { status: "unavailable" },
      global: { stubs },
    });
    expect(attention.classes()).toContain("badge-pill");
  });
});
