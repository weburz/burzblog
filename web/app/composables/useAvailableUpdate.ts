import { computed, ref } from "vue";
import { request } from "../utils/api";
import type { UpdateInfo, Updates } from "../types";

export const dismissedKey = "burzblog:update-dismissed";

// Which update checks found, shared by the Settings page and the nav dot. The
// notice is dismissed per version, remembered in this browser, so the next
// release shows it again. The panel is client-only, so the saved choice is read
// once, on first use.
const enabled = ref(false);
const update = ref<UpdateInfo | null>(null);
const dismissed = ref("");
let loaded = false;

export const useAvailableUpdate = () => {
  if (!loaded) {
    loaded = true;
    try {
      dismissed.value = localStorage.getItem(dismissedKey) ?? "";
    } catch {
      /* storage unavailable */
    }
  }
  // An unreachable or older server just means nothing to show.
  const load = async () => {
    try {
      const reply = await request<Updates>("/api/updates");
      enabled.value = reply.enabled;
      update.value = reply.update;
    } catch {
      enabled.value = false;
      update.value = null;
    }
  };
  const available = computed(() => (update.value?.available ? update.value : null));
  const visible = computed(() =>
    available.value && available.value.latest !== dismissed.value
      ? available.value
      : null,
  );
  const dismiss = () => {
    if (!available.value) return;
    dismissed.value = available.value.latest;
    try {
      localStorage.setItem(dismissedKey, dismissed.value);
    } catch {
      /* storage unavailable */
    }
  };
  return { enabled, update, visible, load, dismiss };
};
