<script setup lang="ts">
import { ref, watch } from "vue";
import { imageUrlError } from "../utils/markdown";

const open = defineModel<boolean>("open", { default: false });
const props = defineProps<{ selected?: string }>();
const emit = defineEmits<{ insert: [alt: string, url: string] }>();
const url = ref("");
const alt = ref("");
const error = ref("");

watch(open, (value) => {
  if (!value) return;
  url.value = "";
  alt.value = props.selected ?? "";
  error.value = "";
});

const insert = () => {
  error.value = imageUrlError(url.value);
  if (error.value) return;
  emit("insert", alt.value, url.value);
  open.value = false;
};
</script>

<template>
  <UModal
    v-model:open="open"
    title="Insert image"
    description="Link to an image that is already online. Uploads are not supported."
    :ui="{ content: 'max-w-lg' }"
  >
    <template #body>
      <form
        id="insert-image"
        class="flex flex-col gap-5"
        novalidate
        @submit.prevent="insert"
      >
        <div class="field">
          <label for="image-url">Image address</label>
          <UInput
            id="image-url"
            v-model="url"
            type="url"
            autofocus
            class="w-full"
            spellcheck="false"
            autocapitalize="none"
            placeholder="https://example.com/photo.jpg"
            :color="error ? 'error' : undefined"
            :aria-invalid="error ? 'true' : undefined"
            :aria-describedby="error ? 'image-url-error' : undefined"
            @input="error = ''"
          />
          <p v-if="error" id="image-url-error" class="field-error" role="alert">
            {{ error }}
          </p>
        </div>
        <div class="field">
          <label for="image-alt">Alt text</label>
          <UInput
            id="image-alt"
            v-model="alt"
            class="w-full"
            aria-describedby="image-alt-hint"
            placeholder="A short description of the image"
          />
          <p id="image-alt-hint" class="field-hint">
            Read aloud by screen readers and shown if the image does not load.
          </p>
        </div>
      </form>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" @click="open = false">Cancel</UButton>
        <UButton type="submit" form="insert-image">Insert image</UButton>
      </div>
    </template>
  </UModal>
</template>
