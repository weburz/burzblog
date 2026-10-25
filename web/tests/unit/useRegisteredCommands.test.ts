import { mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
import { defineComponent, ref } from "vue";
import { useRegisteredCommands } from "../../app/composables/useRegisteredCommands";
import type { PaletteCommand } from "../../app/utils/palette";

const setup = () => {
  let api!: ReturnType<typeof useRegisteredCommands>;
  mount(
    defineComponent({
      setup() {
        api = useRegisteredCommands();
        return () => null;
      },
    }),
  );
  return api;
};

const command = (id: string, run = () => {}): PaletteCommand => ({
  id,
  label: id,
  icon: "i-lucide-check",
  run,
});

describe("useRegisteredCommands", () => {
  it("lists commands from every registered provider", () => {
    const api = setup();
    const stopA = api.register(() => [command("a")]);
    const stopB = api.register(() => [command("b"), command("c")]);
    expect(api.commands.value.map((c) => c.id)).toEqual(["a", "b", "c"]);
    stopA();
    stopB();
  });

  it("takes only its own commands away on unregister", () => {
    const api = setup();
    const stopA = api.register(() => [command("a")]);
    const stopB = api.register(() => [command("b")]);
    stopA();
    expect(api.commands.value.map((c) => c.id)).toEqual(["b"]);
    stopB();
    expect(api.commands.value).toEqual([]);
  });

  it("follows the live state of the page that registered", () => {
    const api = setup();
    const published = ref(false);
    const stop = api.register(() => [command(published.value ? "unpublish" : "publish")]);
    expect(api.commands.value.map((c) => c.id)).toEqual(["publish"]);
    published.value = true;
    expect(api.commands.value.map((c) => c.id)).toEqual(["unpublish"]);
    stop();
  });

  it("runs the page's own function, not a copy", () => {
    const api = setup();
    const save = vi.fn<() => void>();
    const stop = api.register(() => [command("save", save)]);
    api.commands.value[0]?.run();
    expect(save).toHaveBeenCalledOnce();
    stop();
  });

  it("is shared between callers", () => {
    const one = setup();
    const two = setup();
    const stop = one.register(() => [command("a")]);
    expect(two.commands.value.map((c) => c.id)).toEqual(["a"]);
    stop();
    expect(two.commands.value).toEqual([]);
  });
});
