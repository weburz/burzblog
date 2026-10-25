import { onMounted, ref, watch } from "vue";

const storageKey = "burzblog:focus";

// Focus mode for the editor, remembered per browser. The saved choice is read
// after mount so server and client markup agree.
export const useFocusedWriting = () => {
  const focused = ref(false);
  onMounted(() => {
    try {
      focused.value = localStorage.getItem(storageKey) === "1";
    } catch {
      /* storage unavailable */
    }
  });
  watch(focused, (value) => {
    try {
      localStorage.setItem(storageKey, value ? "1" : "0");
    } catch {
      /* storage unavailable */
    }
  });
  const toggle = () => (focused.value = !focused.value);
  return { focused, toggle };
};
