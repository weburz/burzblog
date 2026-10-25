<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from "vue";
import { errorMessage, request } from "../utils/api";
import { previewDocument, type PreviewMeta } from "../utils/preview";

const props = defineProps<{
  body: string;
  meta: PreviewMeta;
  scroll?: number;
  dark?: boolean;
}>();
const frame = ref<HTMLIFrameElement | null>(null);
const error = ref("");
const rendering = ref(false);
let html = "";
let timer: ReturnType<typeof setTimeout> | undefined;
let sequence = 0;

// Rewrite the document in place rather than reloading the frame, so the
// reader's scroll position survives every keystroke.
const paint = () => {
  const doc = frame.value?.contentDocument;
  const next = new DOMParser().parseFromString(
    previewDocument(html, props.meta, props.dark),
    "text/html",
  );
  if (!doc?.body || !doc.querySelector("main")) {
    if (frame.value) frame.value.srcdoc = next.documentElement.outerHTML;
    return;
  }
  doc
    .querySelector("main")!
    .replaceWith(doc.importNode(next.querySelector("main")!, true));
};

const render = async () => {
  const ticket = ++sequence;
  rendering.value = true;
  try {
    const result = await request<{ html: string }>("/api/preview", "POST", {
      markdown: props.body,
    });
    if (ticket !== sequence) return;
    html = result.html;
    error.value = "";
    paint();
  } catch (cause) {
    if (ticket === sequence) error.value = errorMessage(cause);
  } finally {
    if (ticket === sequence) rendering.value = false;
  }
};

watch(
  () => props.body,
  () => {
    clearTimeout(timer);
    timer = setTimeout(render, 250);
  },
);
watch(() => props.meta, paint, { deep: true });
watch(
  () => props.dark,
  (dark) => frame.value?.contentDocument?.documentElement.classList.toggle("dark", dark),
);
watch(
  () => props.scroll,
  (ratio) => {
    const win = frame.value?.contentWindow;
    const doc = frame.value?.contentDocument?.documentElement;
    if (ratio === undefined || !win || !doc) return;
    win.scrollTo({ top: ratio * (doc.scrollHeight - win.innerHeight) });
  },
);
onMounted(() => {
  if (frame.value) frame.value.srcdoc = previewDocument("", props.meta, props.dark);
  void render();
});
onUnmounted(() => clearTimeout(timer));
</script>

<template>
  <div class="preview-pane">
    <p v-if="error" class="preview-error" role="status">
      Preview is out of date: {{ error }}
    </p>
    <!-- No scripts may run inside: the sandbox omits allow-scripts. Same origin
         only so the panel can update it in place and fonts load. -->
    <iframe
      ref="frame"
      class="preview-frame"
      title="Preview of the published post"
      sandbox="allow-same-origin allow-popups allow-popups-to-escape-sandbox"
      :aria-busy="rendering"
    />
  </div>
</template>
