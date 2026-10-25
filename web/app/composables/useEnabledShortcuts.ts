import { ref, watch } from "vue";

export const shortcutsKey = "burzblog:shortcuts";

// One shared switch for the single-key shortcuts ("?", "n" and "/"), so writers
// who type with speech or switch tools can turn them off. Modifier shortcuts
// stay. On by default and remembered per browser; the panel is client-only, so
// the saved choice is read once, on first use.
const enabled = ref(true);
let loaded = false;

export const useEnabledShortcuts = () => {
  if (!loaded) {
    loaded = true;
    try {
      enabled.value = localStorage.getItem(shortcutsKey) !== "0";
    } catch {
      /* storage unavailable */
    }
    watch(enabled, (value) => {
      try {
        localStorage.setItem(shortcutsKey, value ? "1" : "0");
      } catch {
        /* storage unavailable */
      }
    });
  }
  return { enabled };
};
