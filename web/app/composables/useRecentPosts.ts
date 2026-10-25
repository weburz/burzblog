import { ref } from "vue";
import { parseRecent, pushRecent, recentKey } from "../utils/palette";

// The last few posts opened, kept per browser for the command palette. The
// panel is client-only, so the saved list is read once, on first use.
const recent = ref<string[]>([]);
let loaded = false;

export const useRecentPosts = () => {
  if (!loaded) {
    loaded = true;
    try {
      recent.value = parseRecent(localStorage.getItem(recentKey));
    } catch {
      /* storage unavailable */
    }
  }
  const remember = (slug: string) => {
    recent.value = pushRecent(recent.value, slug);
    try {
      localStorage.setItem(recentKey, JSON.stringify(recent.value));
    } catch {
      /* storage unavailable: the list lasts for this visit */
    }
  };
  return { recent, remember };
};
