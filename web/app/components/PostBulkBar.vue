<script setup lang="ts">
import { computed, ref } from "vue";
import type { PostSummary } from "../types";
import { actionLabels, availableActions, type PostAction } from "../utils/post-actions";

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
const submitTag = () => {
  if (!tagText.value.trim()) return;
  emit("tag", tagText.value);
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
    <form v-if="tagging" class="bulk-tag" @submit.prevent="submitTag">
      <UInput
        id="bulk-tag"
        v-model="tagText"
        size="sm"
        placeholder="Tag name"
        aria-label="Tag to add to the selected posts"
        autofocus
        @keydown.esc.stop="tagging = false"
      />
      <UButton type="submit" size="sm" :disabled="busy || !tagText.trim()">Add</UButton>
    </form>
    <UButton size="sm" color="neutral" variant="ghost" @click="emit('clear')"
      >Clear</UButton
    >
  </div>
</template>
