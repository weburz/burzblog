<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import type { Article } from "../types";
import { ApiError, request, errorMessage } from "../utils/api";
import { diffTooLong, lineDiff } from "../utils/diff";
import { editorText, previewDocument, savedSource } from "../utils/editor";

const props = defineProps<{ article: Article }>();
const emit = defineEmits<{ close: [] }>();
const current = ref<Article>(props.article);
const text = ref(editorText(props.article.markdown));
const error = ref("");
const errorHeading = ref("That did not work");
const errorCode = ref("");
const busy = ref(false);
const preview = ref<string | null>(null);
const external = ref<Article | null>(null);
const destination = ref(props.article.path);
const saved = ref(false);
const source = ref<HTMLTextAreaElement | null>(null);
const titleElement = ref<HTMLElement | null>(null);
const confirmingLeave = ref(false);
const confirmingArchive = ref(false);
const confirmingDisk = ref(false);
const stuck = ref(false);
const copied = ref(false);
const copyFailed = ref(false);
let copiedTimer: ReturnType<typeof setTimeout> | undefined;
let titleObserver: IntersectionObserver | undefined;
const dirty = computed(() => text.value !== editorText(current.value.markdown));
const state = computed<"busy" | "dirty" | "saved" | "clean">(() =>
  busy.value ? "busy" : dirty.value ? "dirty" : saved.value ? "saved" : "clean",
);
const errorIcon = computed(() =>
  errorHeading.value === "That did not work" ? "i-lucide-info" : "i-lucide-octagon-alert",
);
const diskDiff = computed(() =>
  external.value ? lineDiff(text.value, editorText(external.value.markdown)) : [],
);
const diskTooLong = computed(() =>
  external.value ? diffTooLong(text.value, editorText(external.value.markdown)) : false,
);
const gutter = { same: "", added: "+", removed: "−" };
const articleStatus = computed(() =>
  current.value.status === "published" ? "published" : "draft",
);
// Browsers without `field-sizing: content` need the textarea sized from its content.
const grow = () => {
  const element = source.value;
  if (
    !element ||
    element.offsetParent === null ||
    (typeof CSS !== "undefined" && CSS.supports("field-sizing", "content"))
  )
    return;
  element.style.height = "auto";
  element.style.height = `${element.scrollHeight}px`;
};
watch(text, () => nextTick(grow));
watch(preview, (value) => {
  if (value === null) nextTick(grow);
});
const accept = (article: Article, saves = true) => {
  const previous = current.value.revision;
  current.value = article;
  text.value = editorText(article.markdown);
  external.value = null;
  if (saves) saved.value = true;
  else if (article.revision !== previous) saved.value = false;
};
const guard = (event: BeforeUnloadEvent) => {
  if (dirty.value) {
    event.preventDefault();
    event.returnValue = "";
  }
};
onMounted(() => {
  titleElement.value?.focus();
  window.addEventListener("beforeunload", guard);
  grow();
  if (titleElement.value && typeof IntersectionObserver !== "undefined") {
    titleObserver = new IntersectionObserver(([entry]) => {
      stuck.value = entry ? !entry.isIntersecting : false;
    });
    titleObserver.observe(titleElement.value);
  }
});
onUnmounted(() => {
  window.removeEventListener("beforeunload", guard);
  titleObserver?.disconnect();
  clearTimeout(copiedTimer);
});
const perform = async (operation: () => Promise<void>, heading = "That did not work") => {
  if (busy.value) return;
  busy.value = true;
  error.value = "";
  try {
    await operation();
  } catch (cause) {
    error.value = errorMessage(cause);
    errorCode.value = cause instanceof ApiError ? cause.code : "";
    errorHeading.value = heading;
  } finally {
    busy.value = false;
  }
};
const save = () =>
  perform(async () => {
    saved.value = false;
    const markdown = savedSource(current.value.markdown, text.value);
    const article = await request<Article>("/api/post", "PUT", {
      path: current.value.path,
      revision: current.value.revision,
      markdown,
    });
    accept(article);
  }, "Save did not happen");
const publish = () =>
  perform(async () => {
    if (dirty.value) {
      throw new Error("Save your writing before publishing.");
    }
    saved.value = false;
    accept(
      await request<Article>("/api/publish", "POST", {
        path: current.value.path,
        revision: current.value.revision,
      }),
    );
  }, "Publish did not happen");
const showPreview = () =>
  perform(async () => {
    const rendered = await request<{ html: string }>("/api/preview", "POST", {
      markdown: savedSource(current.value.markdown, text.value),
    });
    preview.value = previewDocument(rendered.html);
  });
const checkDisk = () =>
  perform(async () => {
    const article = await request<Article>(
      `/api/post?path=${encodeURIComponent(current.value.path)}`,
    );
    if (dirty.value) {
      if (article.revision !== current.value.revision) external.value = article;
    } else {
      accept(article, false);
    }
  });
const rename = () =>
  perform(async () => {
    if (dirty.value) {
      throw new Error("Save or copy unsaved writing before moving the file.");
    }
    saved.value = false;
    accept(
      await request<Article>("/api/rename", "POST", {
        path: current.value.path,
        revision: current.value.revision,
        destination: destination.value,
      }),
    );
  });
const archive = () => {
  confirmingArchive.value = false;
  return perform(async () => {
    if (dirty.value) {
      throw new Error("Save or copy unsaved writing before archiving.");
    }
    await request("/api/archive", "POST", {
      path: current.value.path,
      revision: current.value.revision,
    });
    emit("close");
  });
};
let onLeave: (() => void) | null = null;
const requestLeave = (proceed: () => void) => {
  if (!dirty.value) {
    proceed();
    return;
  }
  onLeave = proceed;
  confirmingLeave.value = true;
};
const leave = () => {
  confirmingLeave.value = false;
  onLeave?.();
  onLeave = null;
};
defineExpose({ dirty, requestLeave });
const copyDisk = async () => {
  if (!external.value) return;
  copyFailed.value = false;
  try {
    await navigator.clipboard.writeText(external.value.markdown);
    copied.value = true;
    clearTimeout(copiedTimer);
    copiedTimer = setTimeout(() => (copied.value = false), 2000);
  } catch {
    copyFailed.value = true;
  }
};
const keepMine = () => {
  external.value = null;
  copied.value = false;
  copyFailed.value = false;
};
const useDisk = () => {
  confirmingDisk.value = false;
  if (external.value) {
    saved.value = false;
    accept(external.value, false);
  }
};
</script>

<template>
  <section class="editor" @keydown.ctrl.s.prevent="save" @keydown.meta.s.prevent="save">
    <div class="editor-header" :class="{ 'is-stuck': stuck }">
      <UButton
        color="neutral"
        variant="ghost"
        :disabled="busy"
        @click="requestLeave(() => emit('close'))"
        >← Posts</UButton
      >
      <span class="editor-crumb" aria-hidden="true">{{
        current.title || current.path
      }}</span>
      <div class="editor-meta">
        <StatusBadge :status="articleStatus" pill />
        <span class="mono editor-path">{{ current.path }}</span>
      </div>
    </div>
    <h1 ref="titleElement" tabindex="-1" class="page-title editor-title">
      {{ current.title || current.path }}
    </h1>
    <div class="editor-modes">
      <div class="editor-column editor-bar">
        <div class="segment" role="group" aria-label="Editor view">
          <UButton
            color="neutral"
            :variant="preview === null ? 'soft' : 'ghost'"
            :aria-pressed="preview === null"
            @click="preview = null"
            >Markdown</UButton
          ><UButton
            color="neutral"
            :variant="preview !== null ? 'soft' : 'ghost'"
            :aria-pressed="preview !== null"
            :disabled="busy"
            @click="showPreview"
            >Preview</UButton
          >
        </div>
        <UTooltip
          text="Reload if the file changed outside BurzBlog. Never replaces unsaved text."
        >
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-hard-drive"
            aria-label="Check disk"
            :disabled="busy"
            @click="checkDisk"
            ><span class="editor-collapse">Check disk</span></UButton
          >
        </UTooltip>
      </div>
    </div>
    <div v-if="error" class="alert editor-column" role="alert">
      <p class="alert-title">
        <UIcon :name="errorIcon" class="size-4 align-text-bottom" />
        {{ errorHeading }}
      </p>
      <p>{{ error }}</p>
      <p v-if="!['authentication', 'network'].includes(errorCode)" class="mt-1">
        Your text is still here. Use Check disk to see what is on disk.
      </p>
    </div>
    <div v-if="external" class="alert alert-warn editor-column conflict" role="alert">
      <h2 class="alert-title">This file changed outside BurzBlog.</h2>
      <p>
        Your unsaved text is still in the editor and nothing on disk was overwritten.
        Compare the two versions, then decide.
      </p>
      <div class="actions mt-3">
        <UButton color="neutral" variant="outline" @click="confirmingDisk = true"
          >Use disk version</UButton
        >
        <UButton color="neutral" variant="ghost" @click="copyDisk">{{
          copied ? "Copied" : "Copy disk version"
        }}</UButton>
        <UButton color="neutral" variant="ghost" @click="keepMine">Keep my text</UButton>
      </div>
      <p v-if="copyFailed" class="mt-2">Select the text below and copy it manually.</p>
      <p v-if="diskTooLong" class="mt-3">
        Too long to compare line by line; showing the disk version.
      </p>
      <p v-else class="diff-legend mt-3">
        <span>− only in your text</span><span>+ only on disk</span>
      </p>
      <div
        class="diff-panel mt-2"
        role="region"
        tabindex="0"
        aria-label="Differences between your text and the disk version"
      >
        <div
          v-for="(line, index) in diskDiff"
          :key="index"
          class="diff-line"
          :class="`diff-${line.kind}`"
        >
          <span class="diff-gutter" aria-hidden="true">{{ gutter[line.kind] }}</span
          ><span class="diff-text">{{ line.text }}</span>
        </div>
      </div>
      <details class="mt-3">
        <summary class="cursor-pointer">Review disk version</summary>
        <pre class="mono mt-3 whitespace-pre-wrap break-words">{{
          external.markdown
        }}</pre>
      </details>
    </div>
    <div v-show="preview === null" class="editor-column">
      <label for="markdown" class="sr-only">Markdown source, including frontmatter</label>
      <textarea
        id="markdown"
        ref="source"
        v-model="text"
        class="editor-source"
        :readonly="busy"
        spellcheck="false"
        aria-describedby="source-hint"
        @input="saved = false"
      />
      <p id="source-hint" class="meta mt-2">
        Plain Markdown with frontmatter. Unknown fields are kept.
      </p>
    </div>
    <div v-if="preview !== null" class="editor-column">
      <p class="meta mt-4">
        Safe preview. Raw HTML and remote images are left out. Nothing is written.
      </p>
      <iframe class="preview" title="Article preview" sandbox="" :srcdoc="preview" />
    </div>
    <div class="editor-footer">
      <div class="editor-column editor-bar">
        <p role="status" aria-live="polite" class="editor-state" :data-state="state">
          <template v-if="state === 'busy'"
            ><UIcon
              name="i-lucide-loader-circle"
              class="size-4 animate-spin"
            />Working…</template
          >
          <template v-else-if="state === 'dirty'"
            ><span class="dot" aria-hidden="true" />Unsaved changes</template
          >
          <template v-else-if="state === 'saved'"
            ><UIcon name="i-lucide-check" class="size-4" />Saved ✓</template
          >
          <template v-else
            ><UIcon name="i-lucide-file-check" class="size-4" />On disk</template
          >
        </p>
        <div class="actions">
          <span class="editor-collapse"><UKbd value="Ctrl" /> <UKbd value="S" /> </span>
          <UTooltip
            v-if="articleStatus !== 'published'"
            text="Save first"
            :disabled="!dirty"
          >
            <span class="inline-flex">
              <UButton
                color="secondary"
                variant="outline"
                :disabled="busy || dirty"
                @click="publish"
                >Publish</UButton
              >
            </span>
          </UTooltip>
          <UButton color="primary" :disabled="busy" @click="save">Save</UButton>
        </div>
      </div>
    </div>
    <details class="file-options editor-column">
      <summary class="muted cursor-pointer">
        File options
        <UIcon name="i-lucide-chevron-down" class="file-options-chevron size-4" />
      </summary>
      <p v-if="dirty" class="meta mt-3">Save first.</p>
      <form class="mt-5" @submit.prevent="rename">
        <label for="destination" class="font-semibold">Move / rename file</label>
        <div class="file-options-row mt-1">
          <UInput id="destination" v-model="destination" class="w-full mono" required />
          <UButton
            type="submit"
            color="neutral"
            variant="outline"
            :disabled="busy || dirty"
            >Move file</UButton
          >
        </div>
        <p class="meta mt-2">
          Changes the filename only; the slug inside frontmatter stays. Existing files are
          never overwritten.
        </p>
      </form>
      <div class="file-options-row file-options-archive">
        <p class="muted">
          Archiving moves the file to .archive/ on disk. It leaves Posts but is not
          deleted.
        </p>
        <UButton
          color="error"
          variant="outline"
          :disabled="busy || dirty"
          @click="confirmingArchive = true"
          >Archive post</UButton
        >
      </div>
    </details>
    <ConfirmDialog
      v-model:open="confirmingLeave"
      title="Leave without saving?"
      body="Unsaved writing will be lost. Copy anything you want to keep first."
      confirm-label="Leave"
      confirm-color="error"
      @confirm="leave"
    />
    <ConfirmDialog
      v-model:open="confirmingArchive"
      title="Archive this post?"
      body="The file moves to .archive/ and disappears from Posts. It stays on disk and can be moved back with ordinary file tools."
      confirm-label="Archive post"
      confirm-color="error"
      @confirm="archive"
    />
    <ConfirmDialog
      v-model:open="confirmingDisk"
      title="Replace your text with the disk version?"
      body="Your unsaved writing in the editor will be discarded. Copy it first if you want to keep it."
      confirm-label="Use disk version"
      confirm-color="error"
      @confirm="useDisk"
    />
  </section>
</template>
