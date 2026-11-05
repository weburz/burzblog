<script setup lang="ts">
import type { MediaKind } from "../types";

// The search box and, unless the kind is fixed, the Images/PDFs filter. The
// parent owns the values and asks the server when they change.
defineProps<{ idPrefix: string; showKind?: boolean }>();
const query = defineModel<string>("query", { required: true });
const kind = defineModel<MediaKind>("kind", { default: "" });
const emit = defineEmits<{ search: []; kind: [] }>();

const kinds: { value: MediaKind; label: string }[] = [
  { value: "", label: "All" },
  { value: "image", label: "Images" },
  { value: "pdf", label: "PDFs" },
];
const choose = (value: MediaKind) => {
  if (kind.value === value) return;
  kind.value = value;
  emit("kind");
};
</script>

<template>
  <div class="media-filters">
    <div class="field media-search">
      <label :for="`${idPrefix}-search`">Search files</label>
      <UInput
        :id="`${idPrefix}-search`"
        v-model="query"
        type="search"
        class="w-full"
        icon="i-lucide-search"
        placeholder="File name or alt text"
        maxlength="200"
        @update:model-value="emit('search')"
      />
    </div>
    <div v-if="showKind" role="group" aria-label="File type" class="media-kinds">
      <UButton
        v-for="option in kinds"
        :key="option.value"
        size="sm"
        color="neutral"
        :variant="kind === option.value ? 'solid' : 'outline'"
        :aria-pressed="kind === option.value"
        @click="choose(option.value)"
        >{{ option.label }}</UButton
      >
    </div>
  </div>
</template>
