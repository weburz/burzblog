<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import type { Article } from "../types";
import { request, errorMessage } from "../utils/api";
import { editorText, previewDocument, savedSource } from "../utils/editor";

const props = defineProps<{ article: Article }>();
const emit = defineEmits<{ close: [] }>();
const current = ref<Article>(props.article);
const text = ref(editorText(props.article.markdown));
const error = ref("");
const busy = ref(false);
const preview = ref<string | null>(null);
const external = ref<Article | null>(null);
const destination = ref(props.article.path);
const saved = ref(false);
const dirty = computed(() => text.value !== editorText(current.value.markdown));
const accept = (article: Article) => {
  current.value = article;
  text.value = editorText(article.markdown);
  external.value = null;
  saved.value = true;
};
const guard = (event: BeforeUnloadEvent) => {
  if (dirty.value) {
    event.preventDefault();
    event.returnValue = "";
  }
};
onMounted(() => window.addEventListener("beforeunload", guard));
onUnmounted(() => window.removeEventListener("beforeunload", guard));
const perform = async (operation: () => Promise<void>) => {
  if (busy.value) return;
  busy.value = true;
  error.value = "";
  saved.value = false;
  try {
    await operation();
  } catch (cause) {
    error.value = errorMessage(cause);
  } finally {
    busy.value = false;
  }
};
const save = () =>
  perform(async () => {
    const source = savedSource(current.value.markdown, text.value);
    const article = await request<Article>("/api/post", "PUT", {
      path: current.value.path,
      revision: current.value.revision,
      markdown: source,
    });
    accept(article);
  });
const publish = () =>
  perform(async () => {
    if (dirty.value) {
      throw new Error("Save your writing before publishing.");
    }
    accept(
      await request<Article>("/api/publish", "POST", {
        path: current.value.path,
        revision: current.value.revision,
      }),
    );
  });
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
      accept(article);
    }
  });
const rename = () =>
  perform(async () => {
    if (dirty.value) {
      throw new Error("Save or copy unsaved writing before moving the file.");
    }
    accept(
      await request<Article>("/api/rename", "POST", {
        path: current.value.path,
        revision: current.value.revision,
        destination: destination.value,
      }),
    );
  });
const archive = () =>
  perform(async () => {
    if (dirty.value) {
      throw new Error("Save or copy unsaved writing before archiving.");
    }
    if (
      !window.confirm(
        "Move this file to the reversible archive? It will disappear from Posts, but remain on disk.",
      )
    ) {
      return;
    }
    await request("/api/archive", "POST", {
      path: current.value.path,
      revision: current.value.revision,
    });
    emit("close");
  });
const close = () => {
  if (
    !dirty.value ||
    window.confirm(
      "Leave without saving? Copy any writing you want to keep before leaving.",
    )
  ) {
    emit("close");
  }
};
const useDisk = () => {
  if (
    external.value &&
    window.confirm(
      "Replace the editor text with the disk version? Copy your unsaved text first.",
    )
  )
    accept(external.value);
};
</script>

<template>
  <section class="panel">
    <div class="toolbar mb-8">
      <UButton color="neutral" variant="ghost" :disabled="busy" @click="close"
        >← Posts</UButton
      ><span class="muted text-sm">{{
        current.status === "published" ? "Published" : "Draft"
      }}</span>
    </div>
    <h1 class="page-title mb-3">{{ current.title }}</h1>
    <p class="muted text-sm mb-8">{{ current.path }}</p>
    <div class="toolbar border-b border-slate-200 pb-3">
      <div class="actions">
        <UButton
          color="neutral"
          :variant="preview === null ? 'soft' : 'ghost'"
          @click="preview = null"
          >Markdown</UButton
        ><UButton
          color="neutral"
          :variant="preview !== null ? 'soft' : 'ghost'"
          :disabled="busy"
          @click="showPreview"
          >Preview</UButton
        >
      </div>
      <UButton color="neutral" variant="ghost" :disabled="busy" @click="checkDisk"
        >Check disk</UButton
      >
    </div>
    <p v-if="error" class="error" role="alert">
      {{ error }}
      <span class="block mt-2"
        >Your editor text is retained. Use Check disk to review the current file.</span
      >
    </p>
    <div v-if="external" class="error" role="alert">
      <h2 class="font-semibold">This file changed outside BurzBlog.</h2>
      <p>
        Your unsaved writing is still below. The current disk version is shown here; copy
        either version before deciding how to reconcile them.
      </p>
      <details class="mt-3">
        <summary>Review disk version</summary>
        <pre class="whitespace-pre-wrap break-words text-sm mt-3">{{
          external.markdown
        }}</pre>
      </details>
      <UButton class="mt-3" color="neutral" @click="useDisk">Use disk version</UButton>
    </div>
    <div v-show="preview === null">
      <label for="markdown" class="sr-only">Markdown source, including frontmatter</label>
      <textarea
        id="markdown"
        v-model="text"
        class="editor-source"
        :readonly="busy"
        spellcheck="false"
        aria-describedby="source-hint"
        @input="saved = false"
        @keydown.ctrl.s.prevent="save"
        @keydown.meta.s.prevent="save"
      />
      <p id="source-hint" class="muted text-xs mb-4">
        Ordinary Markdown, including frontmatter. Unknown fields stay yours. Ctrl / ⌘ S to
        save.
      </p>
    </div>
    <template v-if="preview !== null"
      ><p class="muted text-sm mt-4">
        Safe preview · raw HTML and external images are omitted. Source is unchanged.
      </p>
      <iframe class="preview" title="Article preview" sandbox="" :srcdoc="preview"
    /></template>
    <div class="editor-footer toolbar">
      <p role="status" aria-live="polite" class="text-sm muted">
        {{
          busy
            ? "Working…"
            : dirty
              ? "Unsaved changes"
              : saved
                ? "Saved ✓"
                : "Loaded from disk"
        }}
      </p>
      <div class="actions">
        <UButton color="neutral" variant="outline" :disabled="busy" @click="save"
          >Save</UButton
        ><UButton
          color="secondary"
          :disabled="busy || dirty || current.status === 'published'"
          @click="publish"
          >Publish</UButton
        >
      </div>
    </div>
    <details class="mt-6">
      <summary class="muted cursor-pointer">File options</summary>
      <form class="mt-5 max-w-md" @submit.prevent="rename">
        <div class="field">
          <label for="destination">Move / rename file</label
          ><UInput id="destination" v-model="destination" class="w-full" required />
          <p class="muted text-sm">
            Changes the filename, not the slug inside frontmatter. Existing destinations
            are never overwritten.
          </p>
        </div>
        <UButton type="submit" color="neutral" variant="outline" :disabled="busy || dirty"
          >Move file</UButton
        >
      </form>
      <UButton
        class="mt-6"
        color="error"
        variant="ghost"
        :disabled="busy || dirty"
        @click="archive"
        >Archive post</UButton
      >
    </details>
  </section>
</template>
