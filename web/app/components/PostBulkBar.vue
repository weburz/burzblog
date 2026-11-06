<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useExistingTags } from "../composables/useExistingTags";
import type { PostSummary } from "../types";
import { actionLabels, availableActions, type PostAction } from "../utils/post-actions";
import { normalizeTag } from "../utils/metadata";
import { suggestTags, tagLimitProblem } from "../utils/tags";

const props = defineProps<{
  selected: PostSummary[];
  admin: boolean;
  busy: boolean;
}>();
const emit = defineEmits<{
  act: [action: PostAction];
  tag: [tag: string];
  clear: [];
}>();

const actions = computed(() => availableActions(props.selected, props.admin));
const tagging = ref(false);
const tagText = ref("");
const { existing, refresh } = useExistingTags();
watch(tagging, (open) => open && refresh());
const tagItems = computed(() =>
  suggestTags(existing.value, tagText.value, []).map((t) => ({
    label: t.tag,
    value: t.tag,
    count: t.count,
    description: t.label.toLowerCase() === t.tag ? undefined : t.label,
  })),
);
const postsLabel = (count: number) => `${count} ${count === 1 ? "post" : "posts"}`;
const tagProblem = computed(() => tagLimitProblem([], tagText.value));
const submitTag = (raw = tagText.value) => {
  if (!raw.trim() || tagLimitProblem([], raw)) return;
  emit("tag", raw);
  tagText.value = "";
  tagging.value = false;
};
</script>

<template>
  <div class="bulk-bar" role="group" aria-label="Actions for the selected posts">
    <span class="bulk-count" role="status">{{ selected.length }} selected</span>
    <UButton
      v-for="action in actions"
      :key="action"
      size="sm"
      :color="action === 'delete' ? 'error' : 'neutral'"
      variant="outline"
      :disabled="busy"
      @click="emit('act', action)"
      >{{ actionLabels[action] }}</UButton
    >
    <UButton
      size="sm"
      color="neutral"
      variant="outline"
      icon="i-lucide-tag"
      :disabled="busy"
      :aria-expanded="tagging"
      @click="tagging = !tagging"
      >Add tag</UButton
    >
    <form v-if="tagging" class="bulk-tag" @submit.prevent="submitTag()">
      <UInputMenu
        id="bulk-tag"
        v-model:search-term="tagText"
        :items="tagItems"
        value-key="value"
        :create-item="tagProblem ? false : 'always'"
        open-on-focus
        :trailing-icon="false"
        size="sm"
        placeholder="Tag name"
        aria-label="Tag to add to the selected posts"
        :aria-describedby="tagProblem ? 'bulk-tag-problem' : undefined"
        autofocus
        @update:model-value="submitTag"
        @create="submitTag()"
        @keydown.esc.stop="tagging = false"
      >
        <template #item-trailing="{ item }">
          <span class="field-hint">{{ postsLabel(item.count) }}</span>
        </template>
        <template #create-item-label="{ item }">Add “{{ normalizeTag(item) }}”</template>
        <template #empty>Type to add a new tag.</template>
      </UInputMenu>
      <UButton type="submit" size="sm" :disabled="busy || !tagText.trim() || !!tagProblem"
        >Add</UButton
      >
      <span v-if="tagProblem" id="bulk-tag-problem" class="field-error" role="status">{{
        tagProblem
      }}</span>
    </form>
    <UButton size="sm" color="neutral" variant="ghost" @click="emit('clear')"
      >Clear</UButton
    >
  </div>
</template>
