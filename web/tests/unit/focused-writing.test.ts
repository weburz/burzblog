import { mount } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";
import { defineComponent, nextTick } from "vue";
import { useFocusedWriting } from "../../app/composables/useFocusedWriting";

const setup = () => {
  let api!: ReturnType<typeof useFocusedWriting>;
  mount(
    defineComponent({
      setup() {
        api = useFocusedWriting();
        return () => null;
      },
    }),
  );
  return api;
};

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("useFocusedWriting", () => {
  it("starts off and remembers the choice", async () => {
    const api = setup();
    expect(api.focused.value).toBe(false);
    api.toggle();
    await nextTick();
    expect(localStorage.getItem("burzblog:focus")).toBe("1");
    api.toggle();
    await nextTick();
    expect(localStorage.getItem("burzblog:focus")).toBe("0");
  });

  it("restores a saved choice after mount", () => {
    localStorage.setItem("burzblog:focus", "1");
    expect(setup().focused.value).toBe(true);
  });

  it("works when storage is unavailable", async () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("denied");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("denied");
    });
    const api = setup();
    expect(api.focused.value).toBe(false);
    api.toggle();
    await nextTick();
    expect(api.focused.value).toBe(true);
  });
});
