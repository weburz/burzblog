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

  it("shows a draft with a publish time as scheduled", () => {
    const wrapper = mount(StatusBadge, {
      props: { status: "draft", publishAt: "2026-11-03T09:00:00Z" },
    });
    expect(wrapper.text()).toBe("Scheduled");
    expect(wrapper.classes()).toContain("badge-scheduled");
  });

  it("ignores a publish time on a post that is not a draft", () => {
    const wrapper = mount(StatusBadge, {
      props: { status: "published", publishAt: "2026-11-03T09:00:00Z" },
    });
    expect(wrapper.text()).toBe("Published");
  });
});
