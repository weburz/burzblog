<script setup lang="ts">
import { ref } from "vue";

// An upload's alt text, edited in place: Enter or leaving the field saves it,
// Escape puts the saved text back. save resolves to why it failed, or to an
// empty string; after a failure the typed text stays, with a Retry beside it.
const props = defineProps<{
  inputId: string;
  alt: string;
  label: string;
  save: (alt: string) => Promise<string>;
}>();
const draft = ref<string | null>(null);
const error = ref("");
const saving = ref(false);

const shown = () => draft.value ?? props.alt;
const edit = (text: string) => {
  draft.value = text;
  error.value = "";
};
const commit = async () => {
  if (saving.value) return;
  const text = draft.value?.trim();
  if (text === undefined || text === props.alt) {
    draft.value = null;
    error.value = "";
    return;
  }
  saving.value = true;
  error.value = await props.save(text);
  saving.value = false;
  if (!error.value) draft.value = null;
};
// Leaving the field after a failure does not try again by itself; Enter and
// Retry do, so moving to the Retry button does not send twice.
const leave = () => {
  if (!error.value) void commit();
};
// Focus stays in the field; blurring it would drop focus to the page.
const cancel = () => {
  draft.value = null;
  error.value = "";
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
      :aria-invalid="error ? 'true' : undefined"
      placeholder="Describe the image"
      @update:model-value="edit"
      @keydown.enter.prevent="commit"
      @keydown.esc.prevent="cancel"
      @blur="leave"
    />
    <p v-if="error" class="field-hint" role="alert">
      Not saved. {{ error }}
      <UButton
        size="xs"
        color="neutral"
        variant="outline"
        :loading="saving"
        :aria-label="`Retry saving the alt text for ${label}`"
        @click="commit"
        >Retry</UButton
      >
    </p>
  </div>
</template>
