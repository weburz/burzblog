import { mount } from "@vue/test-utils";
import { defineComponent, h } from "vue";
import { describe, expect, it, vi } from "vitest";

vi.stubGlobal("useCurrentSession", () => ({ signIn: vi.fn<() => void>() }));
const { default: LoginForm } = await import("../../app/components/LoginForm.vue");

const stub = (tag: string) =>
  defineComponent({
    inheritAttrs: false,
    setup:
      (_, { attrs, slots }) =>
      () =>
        h(tag, attrs, slots.default?.()),
  });

describe("login form forgot-password note", () => {
  it("explains the server-side reset with a copyable command", () => {
    const wrapper = mount(LoginForm, {
      props: { username: "sagar" },
      global: {
        stubs: {
          UInput: stub("input"),
          UButton: stub("button"),
          PasswordInput: stub("input"),
          CopyField: defineComponent({
            props: ["value", "label"],
            setup: (props) => () => h("pre", props.value),
          }),
        },
      },
    });
    const details = wrapper.get("details");
    expect(details.get("summary").text()).toBe("Forgot your password?");
    expect(details.text()).toContain("whoever runs BurzBlog");
    expect(details.get("pre").text()).toBe("burzblog user passwd sagar");
  });
});
