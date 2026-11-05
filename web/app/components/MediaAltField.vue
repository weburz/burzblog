<script setup lang="ts">
import { ref } from "vue";

// An upload's alt text, edited in place: Enter or leaving the field saves it,
// Escape puts the saved text back and keeps focus. The parent does the saving.
const props = defineProps<{ inputId: string; alt: string; label: string }>();
const emit = defineEmits<{ save: [alt: string] }>();
const draft = ref<string | null>(null);

const shown = () => draft.value ?? props.alt;
const commit = () => {
  const text = draft.value?.trim();
  draft.value = null;
  if (text !== undefined && text !== props.alt) emit("save", text);
};
// Focus stays in the field; blurring it would drop focus to the page.
const cancel = () => {
  draft.value = null;
};
</script>

<template>
  <div class="field media-alt-field">
    <label :for="inputId" class="media-alt-label">Alt text</label>
    <UInput
      :id="inputId"
      :model-value="shown()"
      class="w-full"
      maxlength="500"
      :aria-label="`Alt text for ${label}`"
      placeholder="Describe the image"
      @update:model-value="draft = $event"
      @keydown.enter.prevent="commit"
      @keydown.esc.prevent="cancel"
      @blur="commit"
    />
  </div>
</template>
