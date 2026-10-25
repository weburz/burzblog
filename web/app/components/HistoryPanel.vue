<script setup lang="ts">
import { ref, watch } from "vue";
import type { Revision, RevisionList, RevisionState, RevisionSummary } from "../types";
import { errorMessage, request } from "../utils/api";
import { changeSummary } from "../utils/changes";
import { lineDiff, diffTooLong } from "../utils/diff";
import { editedLabel, exactTime } from "../utils/time";

const open = defineModel<boolean>("open", { default: false });
const props = defineProps<{
  slug: string;
  version: number;
  body: string;
  dirty: boolean;
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

// Each version is described by the save that came after it; the newest is
// compared with the post as it is now.
const summaryOf = (index: number): string => {
  const list = items.value;
  const after = list?.[index - 1] ?? current.value;
  return list && after ? changeSummary(list[index]!, after) : "";
};

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
          <li v-for="(rev, index) in items" :key="rev.version">
            <button
              type="button"
              class="history-row"
              :disabled="loadingOne"
              :aria-label="`Version ${rev.version}, ${rev.title || 'Untitled'}: ${summaryOf(index)}. ${rev.status}, saved ${exactTime(rev.savedAt)}${rev.savedBy ? ` by ${rev.savedBy}` : ''}`"
              @click="pick(rev)"
            >
              <span class="history-version">v{{ rev.version }}</span>
              <span class="history-title">{{ rev.title || "Untitled" }}</span>
              <span class="history-change">{{ summaryOf(index) }}</span>
              <StatusBadge :status="rev.status" />
              <time :datetime="rev.savedAt" :title="exactTime(rev.savedAt)" class="muted"
                >{{ editedLabel(rev.savedAt)
                }}<template v-if="rev.savedBy"> by {{ rev.savedBy }}</template></time
              >
            </button>
          </li>
        </ol>
        <p v-else-if="!error" class="loading" role="status">Loading history…</p>
      </template>
    </template>
  </USlideover>
</template>
