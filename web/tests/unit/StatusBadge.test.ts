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
});
