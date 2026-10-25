import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import StatusBadge from "../../app/components/StatusBadge.vue";

describe("status badge", () => {
  it.each([
    ["draft", "Draft"],
    ["published", "Published"],
    ["archived", "Archived"],
  ] as const)("renders %s as %s", (status, text) => {
    const wrapper = mount(StatusBadge, { props: { status } });
    expect(wrapper.text()).toBe(text);
    expect(wrapper.classes()).toContain(`badge-${status}`);
  });
});
