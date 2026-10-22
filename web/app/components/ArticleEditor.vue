<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import type { Article } from "../types";
import { ApiError, request, errorMessage } from "../utils/api";
import { diffTooLong, lineDiff } from "../utils/diff";
import { editorText, previewDocument, savedSource } from "../utils/editor";
import { withRetainedFocus } from "../utils/focus";
import { pathProblem } from "../utils/posts";

// Each failure is shown beside the control that caused it: the mode row (top), the
// sticky save bar (footer) or the file option itself.
type Place = "top" | "footer" | "rename" | "archive";

const props = defineProps<{ article: Article }>();
const toast = useToast();
const emit = defineEmits<{
  close: [];
  archived: [path: string];
  changed: [article: Article];
}>();
const current = ref<Article>(props.article);
const text = ref(editorText(props.article.markdown));
const failure = ref<{
  place: Place;
  heading: string;
  message: string;
  code: string;
} | null>(null);
const busy = ref(false);
const preview = ref<string | null>(null);
const external = ref<Article | null>(null);
const destination = ref(props.article.path);
const saved = ref(false);
const source = ref<HTMLTextAreaElement | null>(null);
const titleElement = ref<HTMLElement | null>(null);
const conflictElement = ref<HTMLElement | null>(null);
const conflictHeading = ref<HTMLElement | null>(null);
const fileFailure = ref<HTMLElement | null>(null);
const previewFrame = ref<HTMLIFrameElement | null>(null);
// The preview grows with its content, so the page has one scrollbar. The frame
// may be read because it is same-origin, but it still runs no scripts.
const sizePreview = () => {
  const frame = previewFrame.value;
  const page = frame?.contentDocument?.documentElement;
  if (frame && page) frame.style.height = `${page.scrollHeight}px`;
};
let previewWidth: ResizeObserver | undefined;
watch(previewFrame, (frame) => {
  previewWidth?.disconnect();
  if (frame && typeof ResizeObserver !== "undefined") {
    previewWidth = new ResizeObserver(sizePreview);
    previewWidth.observe(frame);
  }
});
const confirmingLeave = ref(false);
const confirmingArchive = ref(false);
const confirmingDisk = ref(false);
const confirmingUnpublish = ref(false);
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
  failure.value?.heading === "That did not work"
    ? "i-lucide-info"
    : "i-lucide-octagon-alert",
);
const failureHint = computed(() => {
  const value = failure.value;
  if (!value || value.code === "network") return "";
  if (value.code === "authentication") return "Your text is still here.";
  if (value.place !== "footer") return "";
  // A refusal of the text itself is fixed in the editor, not by reloading disk.
  return value.code === "invalid" || value.code === "validation"
    ? "Your text is still here. Correct it and save again."
    : "Your text is still here. Use Check disk to see what is on disk.";
});
const moveProblem = computed(() => {
  const target = destination.value.trim();
  return target === current.value.path ? "" : pathProblem(target);
});
const diskDiff = computed(() =>
  external.value ? lineDiff(text.value, editorText(external.value.markdown)) : [],
);
const diskTooLong = computed(() =>
  external.value ? diffTooLong(text.value, editorText(external.value.markdown)) : false,
);
const gutter = { same: "", added: "+", removed: "−" };
const articleStatus = computed(() =>
  current.value.problem
    ? "unavailable"
    : current.value.status === "published"
      ? "published"
      : "draft",
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
// A comparison that opens far from where the writer is (a refused save from the sticky
// bar) is brought into view and focused, because it needs a decision.
watch(external, async (value, previous) => {
  if (!value || previous) return;
  await nextTick();
  const heading = conflictHeading.value;
  if (!heading) return;
  const { top } = heading.getBoundingClientRect();
  if (top < 72 || top > window.innerHeight - 120) {
    const smooth = !window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
    conflictElement.value?.scrollIntoView({
      block: "start",
      behavior: smooth ? "smooth" : "auto",
    });
  }
  heading.focus({ preventScroll: true });
});
// File options sit below the sticky save bar; keep their refusal visible.
watch(
  () => failure.value?.place,
  async (place) => {
    if (place !== "rename" && place !== "archive") return;
    await nextTick();
    fileFailure.value?.scrollIntoView({ block: "nearest" });
  },
);
const accept = (article: Article, saves = true) => {
  const previous = current.value.revision;
  current.value = article;
  emit("changed", article);
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
const restored = () => {
  if (failure.value?.code === "authentication") failure.value = null;
};
onMounted(() => {
  titleElement.value?.focus();
  window.addEventListener("beforeunload", guard);
  window.addEventListener("burzblog:session-restored", restored);
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
  window.removeEventListener("burzblog:session-restored", restored);
  titleObserver?.disconnect();
  previewWidth?.disconnect();
  clearTimeout(copiedTimer);
});
const perform = (
  operation: () => Promise<void>,
  place: Place = "top",
  heading = "That did not work",
  describe: (cause: unknown) => string = errorMessage,
) =>
  withRetainedFocus(
    async () => {
      if (busy.value) return;
      busy.value = true;
      failure.value = null;
      try {
        await operation();
      } catch (cause) {
        failure.value = {
          place,
          heading,
          message: describe(cause),
          code: cause instanceof ApiError ? cause.code : "",
        };
      } finally {
        busy.value = false;
      }
    },
    () => [source.value, titleElement.value],
  );
const save = () =>
  perform(
    async () => {
      saved.value = false;
      const markdown = savedSource(current.value.markdown, text.value);
      try {
        const article = await request<Article>("/api/post", "PUT", {
          path: current.value.path,
          revision: current.value.revision,
          markdown,
        });
        accept(article);
      } catch (cause) {
        // A refused save compares at once when the file really changed on disk, or
        // simply loads it when nothing here was edited; any other failure keeps the
        // error block.
        if (cause instanceof ApiError && cause.code === "conflict") {
          const onDisk = await request<Article>(
            `/api/post?path=${encodeURIComponent(current.value.path)}`,
          ).catch(() => null);
          if (onDisk && onDisk.revision !== current.value.revision) {
            if (dirty.value) external.value = onDisk;
            else accept(onDisk, false);
            return;
          }
        }
        throw cause;
      }
    },
    "footer",
    "Save did not happen",
  );
const publish = () =>
  perform(
    async () => {
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
    },
    "footer",
    "Publish did not happen",
  );
const unpublish = () => {
  confirmingUnpublish.value = false;
  return perform(
    async () => {
      if (dirty.value) throw new Error("Save your writing before unpublishing.");
      saved.value = false;
      accept(
        await request<Article>("/api/unpublish", "POST", {
          path: current.value.path,
          revision: current.value.revision,
        }),
      );
    },
    "footer",
    "Unpublish did not happen",
  );
};
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
const folderOf = (path: string) => path.slice(0, path.lastIndexOf("/") + 1);
const describeMove = (cause: unknown) => {
  const target = destination.value.trim();
  if (cause instanceof ApiError && cause.code === "exists")
    return `${target} already exists. Nothing was overwritten; choose another name.`;
  if (cause instanceof ApiError && cause.code === "not_found")
    return folderOf(target)
      ? `Nothing was moved. The folder ${folderOf(target)} does not exist, or this file is no longer on disk. BurzBlog does not create folders.`
      : "Nothing was moved. This file is no longer on disk.";
  return errorMessage(cause);
};
const rename = () =>
  perform(
    async () => {
      if (dirty.value) {
        throw new Error("Save or copy unsaved writing before moving the file.");
      }
      if (moveProblem.value) throw new Error(moveProblem.value);
      saved.value = false;
      accept(
        await request<Article>("/api/rename", "POST", {
          path: current.value.path,
          revision: current.value.revision,
          destination: destination.value.trim(),
        }),
      );
      destination.value = current.value.path;
      toast.add({
        title: "File moved",
        description: current.value.path,
        color: "success",
      });
    },
    "rename",
    "The file was not moved",
    describeMove,
  );
const archive = () => {
  confirmingArchive.value = false;
  return perform(
    async () => {
      if (dirty.value) {
        throw new Error("Save or copy unsaved writing before archiving.");
      }
      await request("/api/archive", "POST", {
        path: current.value.path,
        revision: current.value.revision,
      });
      emit("archived", current.value.path);
    },
    "archive",
    "The post was not archived",
    (cause) =>
      cause instanceof ApiError && cause.code === "not_found"
        ? "This file is no longer on disk; it may have been moved or deleted outside BurzBlog."
        : errorMessage(cause),
  );
};
let onLeave: (() => void) | null = null;
let onStay: (() => void) | null = null;
// `stay` runs when the writer keeps editing (Cancel, Escape or clicking outside),
// so a browser Back that was already applied can be undone by the caller.
const requestLeave = (proceed: () => void, stay?: () => void) => {
  if (!dirty.value) {
    proceed();
    return;
  }
  onLeave = proceed;
  onStay = stay ?? null;
  confirmingLeave.value = true;
};
watch(confirmingLeave, (open) => {
  if (open || !onStay) return;
  const stay = onStay;
  onLeave = null;
  onStay = null;
  stay();
});
const leave = () => {
  const proceed = onLeave;
  onLeave = null;
  onStay = null;
  confirmingLeave.value = false;
  proceed?.();
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
        current.title || "Untitled"
      }}</span>
      <div class="editor-meta">
        <StatusBadge :status="articleStatus" pill />
        <span class="mono editor-path" :title="current.path">{{ current.path }}</span>
      </div>
    </div>
    <div class="editor-column">
      <h1
        ref="titleElement"
        tabindex="-1"
        class="page-title editor-title"
        :class="{ 'post-untitled': !current.title }"
      >
        {{ current.title || "Untitled" }}
      </h1>
    </div>
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
          portal="#main"
          :ui="{ content: 'h-auto max-w-[calc(100vw-16px)]', text: 'whitespace-normal' }"
        >
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-hard-drive"
            :disabled="busy"
            @click="checkDisk"
            >Check disk</UButton
          >
        </UTooltip>
      </div>
    </div>
    <div v-if="failure?.place === 'top'" class="alert editor-column" role="alert">
      <p class="alert-title">
        <UIcon :name="errorIcon" class="size-4 align-text-bottom" />
        {{ failure.heading }}
      </p>
      <p>{{ failure.message }}</p>
      <p v-if="failureHint" class="mt-1">{{ failureHint }}</p>
    </div>
    <div
      v-if="external"
      ref="conflictElement"
      class="alert alert-warn editor-column conflict"
      role="alert"
    >
      <h2 ref="conflictHeading" tabindex="-1" class="alert-title">
        This file changed outside BurzBlog.
      </h2>
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
    <div
      v-if="current.problem"
      class="alert alert-warn editor-column"
      role="status"
      aria-live="polite"
    >
      <p class="alert-title">
        <UIcon name="i-lucide-triangle-alert" class="size-4 align-text-bottom" />
        BurzBlog cannot read this file's frontmatter
      </p>
      <p>{{ current.problem }}</p>
      <p class="mt-1">
        Fix it in the source below and save. The current file is kept as a recovery copy,
        and Publish stays off until BurzBlog can read the frontmatter.
      </p>
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
      <iframe
        ref="previewFrame"
        class="preview"
        title="Article preview"
        sandbox="allow-same-origin"
        :srcdoc="preview"
        @load="sizePreview"
      />
    </div>
    <div class="editor-footer">
      <div
        v-if="failure?.place === 'footer'"
        class="alert editor-column footer-alert"
        role="alert"
      >
        <div>
          <p class="alert-title">
            <UIcon :name="errorIcon" class="size-4 align-text-bottom" />
            {{ failure.heading }}
          </p>
          <p>{{ failure.message }}</p>
          <p v-if="failureHint" class="mt-1">{{ failureHint }}</p>
        </div>
        <UButton
          color="neutral"
          variant="ghost"
          size="sm"
          icon="i-lucide-x"
          aria-label="Dismiss"
          @click="failure = null"
        />
      </div>
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
          <span class="editor-collapse"><UKbd value="meta" /> <UKbd value="S" /> </span>
          <UTooltip
            v-if="articleStatus === 'draft'"
            :text="
              dirty
                ? 'Save first'
                : 'Mark for publication. Deploy your website separately.'
            "
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
          <UTooltip
            v-else-if="articleStatus === 'published'"
            text="Save first"
            :disabled="!dirty"
          >
            <span class="inline-flex">
              <UButton
                color="neutral"
                variant="outline"
                :disabled="busy || dirty"
                @click="confirmingUnpublish = true"
                >Unpublish</UButton
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
          <UInput
            id="destination"
            v-model="destination"
            class="w-full mono"
            required
            :aria-invalid="moveProblem ? 'true' : undefined"
            aria-describedby="destination-hint"
            @update:model-value="failure?.place === 'rename' && (failure = null)"
          />
          <UButton
            type="submit"
            color="neutral"
            variant="outline"
            :disabled="
              busy || dirty || !!moveProblem || destination.trim() === current.path
            "
            >Move file</UButton
          >
        </div>
        <p v-if="moveProblem" class="meta mt-2 text-[var(--bb-danger)]">
          {{ moveProblem }}
        </p>
        <p id="destination-hint" class="meta mt-2">
          Changes the filename only; the slug inside frontmatter stays. Existing files are
          never overwritten.
        </p>
        <div
          v-if="failure?.place === 'rename'"
          ref="fileFailure"
          class="alert file-failure"
          role="alert"
        >
          <p class="alert-title">{{ failure.heading }}</p>
          <p>{{ failure.message }}</p>
        </div>
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
      <div
        v-if="failure?.place === 'archive'"
        ref="fileFailure"
        class="alert file-failure"
        role="alert"
      >
        <p class="alert-title">{{ failure.heading }}</p>
        <p>{{ failure.message }}</p>
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
      v-model:open="confirmingUnpublish"
      title="Move this post back to drafts?"
      body="Only its status changes to draft; the text and every other field stay as they are. Publishing again is one click."
      confirm-label="Unpublish"
      @confirm="unpublish"
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
