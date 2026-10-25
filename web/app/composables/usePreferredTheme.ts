import { watch } from "vue";
import {
  asThemePreference,
  isDark,
  themeKey,
  type ThemePreference,
} from "../utils/theme";

// The writer's light/dark choice. "System" follows the operating system, live.
// Stored per browser: it is a viewing preference, not account data.
export const usePreferredTheme = () => {
  const preference = useState<ThemePreference>("theme", () => {
    try {
      return asThemePreference(localStorage.getItem(themeKey));
    } catch {
      return "system";
    }
  });
  const dark = useState("theme-dark", () => false);

  const start = () => {
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const apply = () => {
      dark.value = isDark(preference.value, media.matches);
      document.documentElement.classList.toggle("dark", dark.value);
    };
    apply();
    media.addEventListener("change", apply);
    watch(preference, (value) => {
      try {
        if (value === "system") localStorage.removeItem(themeKey);
        else localStorage.setItem(themeKey, value);
      } catch {
        /* storage unavailable: the choice lasts for this visit */
      }
      apply();
    });
    return () => media.removeEventListener("change", apply);
  };

  return { preference, dark, start };
};
