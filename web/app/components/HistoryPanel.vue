<script setup lang="ts">
import { computed, ref, watch } from "vue";
import type { Revision, RevisionList, RevisionState, RevisionSummary } from "../types";
import { errorMessage, request } from "../utils/api";
import { changeSummary, showsTitle } from "../utils/changes";
import { lineDiff, diffTooLong } from "../utils/diff";
import { editedLabel, exactTime } from "../utils/time";

const open = defineModel<boolean>("open", { default: false });
const props = defineProps<{
  slug: string;
  version: number;
  body: string;
  dirty: boolean;
  closeFocus?: (event: Event) => void;
}>();
const emit = defineEmits<{ restore: [version: number] }>();

const items = ref<RevisionSummary[] | null>(null);
const current = ref<RevisionState | null>(null);
const error = ref("");
const selected = ref<Revision | null>(null);
const loadingOne = ref(false);

watch(open, async (value) => {
  if (!value) return;
  selected.value = null;
  items.value = null;
  error.value = "";
  try {
    const list = await request<RevisionList>(`/api/posts/${props.slug}/revisions`);
    current.value = list.current;
    items.value = list.items;
  } catch (cause) {
    error.value = errorMessage(cause);
  }
});

// Each row says what that version changed compared with the one before it.
// The oldest has nothing to compare with: v1 is where the post began, and an
// older start means the list was cut off.
const summaryOf = (index: number): string => {
  const list = items.value;
  const rev = list?.[index];
  if (!list || !rev) return "";
  const before = list[index + 1];
  if (before) return changeSummary(before, rev);
  return rev.version === 1 ? "Created" : "";
};
// The post as it is now heads the list, with what the latest save changed.
const currentSummary = computed(() => {
  const latest = items.value?.[0];
  if (!current.value) return "";
  return latest ? changeSummary(latest, current.value) : "Created";
});

const pick = async (rev: RevisionSummary) => {
  loadingOne.value = true;
  error.value = "";
  try {
    selected.value = await request<Revision>(
      `/api/posts/${props.slug}/revisions/${rev.version}`,
    );
  } catch (cause) {
    error.value = errorMessage(cause);
  } finally {
    loadingOne.value = false;
  }
};
</script>

<template>
  <USlideover
    v-model:open="open"
    title="History"
    :description="`Every save is kept. You are on version ${version}.`"
    :ui="{ content: 'max-w-2xl' }"
    :content="{ onCloseAutoFocus: closeFocus }"
  >
    <template #body>
      <p v-if="error" class="alert" role="alert">{{ error }}</p>
      <template v-if="selected?.post">
        <div class="history-head">
          <UButton
            color="neutral"
            variant="ghost"
            icon="i-lucide-arrow-left"
            @click="selected = null"
            >All versions</UButton
          >
        </div>
        <h3 class="section-title">
          Version {{ selected.version }}: {{ selected.post.title || "Untitled" }}
        </h3>
        <p class="muted">
          Saved {{ exactTime(selected.savedAt) }}
          <template v-if="selected.savedBy">by {{ selected.savedBy }}</template>
        </p>
        <p class="field-hint mt-4">
          Changes from that version to what is in your editor now:
        </p>
        <div v-if="diffTooLong(selected.post.body, body)" class="quiet-block">
          This post is too long to compare line by line.
        </div>
        <div
          v-else
          class="diff-panel"
          role="region"
          aria-label="Differences"
          tabindex="0"
        >
          <div
            v-for="(line, i) in lineDiff(selected.post.body, body)"
            :key="i"
            class="diff-line"
            :class="`diff-${line.kind}`"
          >
            <span class="diff-gutter" aria-hidden="true">{{
              line.kind === "added" ? "+" : line.kind === "removed" ? "−" : ""
            }}</span>
            <span class="diff-text"
              ><span v-if="line.kind !== 'same'" class="sr-only">{{
                line.kind === "added" ? "Now: " : "Then: "
              }}</span
              >{{ line.text || " " }}</span
            >
          </div>
        </div>
        <div class="history-actions">
          <UButton
            color="secondary"
            icon="i-lucide-rotate-ccw"
            @click="emit('restore', selected.version)"
            >Restore version {{ selected.version }}</UButton
          >
          <p class="field-hint">
            Restoring saves it as a new version; nothing is lost.
            <template v-if="dirty"> Your unsaved changes will be replaced.</template>
          </p>
        </div>
      </template>
      <template v-else>
        <p v-if="items && items.length === 0" class="muted">
          No earlier versions yet. They appear here each time you save.
        </p>
        <ol v-else-if="items" class="history-list">
          <li v-if="current">
            <div class="history-row history-current" aria-current="true">
              <span class="history-version">v{{ version }}</span>
              <span class="history-change">{{ currentSummary }}</span>
              <StatusBadge :status="current.status" />
              <span class="muted">Current version</span>
              <span class="history-title">“{{ current.title || "Untitled" }}”</span>
            </div>
          </li>
          <li v-for="(rev, index) in items" :key="rev.version">
            <button
              type="button"
              class="history-row"
              :disabled="loadingOne"
              :aria-label="`Version ${rev.version}: ${summaryOf(index)}${showsTitle(items, index) ? `, titled ${rev.title || 'Untitled'}` : ''}. ${rev.status}, saved ${exactTime(rev.savedAt)}${rev.savedBy ? ` by ${rev.savedBy}` : ''}`"
              @click="pick(rev)"
            >
              <span class="history-version">v{{ rev.version }}</span>
              <span class="history-change">{{ summaryOf(index) }}</span>
              <StatusBadge :status="rev.status" />
              <time :datetime="rev.savedAt" :title="exactTime(rev.savedAt)" class="muted"
                >{{ editedLabel(rev.savedAt)
                }}<template v-if="rev.savedBy"> by {{ rev.savedBy }}</template></time
              >
              <span v-if="showsTitle(items, index)" class="history-title"
                >“{{ rev.title || "Untitled" }}”</span
              >
            </button>
          </li>
        </ol>
        <p v-else-if="!error" class="loading" role="status">Loading history…</p>
      </template>
    </template>
  </USlideover>
</template>
