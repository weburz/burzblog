import { computed, shallowRef } from "vue";
import type { PaletteCommand } from "../utils/palette";

// A page hands the command palette actions of its own (the editor's Save and
// Publish). Each page registers a function, so the list follows its live state.
type Provider = () => PaletteCommand[];

const providers = shallowRef<Provider[]>([]);

export const useRegisteredCommands = () => {
  const commands = computed(() => providers.value.flatMap((provider) => provider()));
  // Returns the function that takes the commands away again.
  const register = (provider: Provider) => {
    providers.value = [...providers.value, provider];
    return () => {
      providers.value = providers.value.filter((p) => p !== provider);
    };
  };
  return { commands, register };
};
