import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import ShortcutsDialog from "../../app/components/ShortcutsDialog.vue";
import { shortcutGroups } from "../../app/utils/shortcuts";

describe("shortcuts dialog", () => {
  it("lists every shortcut with its keys", () => {
    const wrapper = mount(ShortcutsDialog, {
      props: { open: true },
      global: {
        stubs: {
          UModal: { template: "<div><slot name='body' /></div>" },
          UKbd: {
            props: ["value"],
            template: "<kbd>{{ value }}</kbd>",
          },
        },
      },
    });
    const rows = wrapper.findAll(".shortcut-row");
    const total = shortcutGroups.reduce((sum, group) => sum + group.items.length, 0);
    expect(rows).toHaveLength(total);
    const save = rows.find((row) => row.text().startsWith("Save"));
    expect(save?.findAll("kbd").map((key) => key.text())).toEqual(["meta", "s"]);
    expect(wrapper.text()).toContain("Focus mode");
    expect(wrapper.text()).toContain("Continue the list");
  });
});
