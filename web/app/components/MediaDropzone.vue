<script setup lang="ts">
import { ref } from "vue";
import { acceptedTypes, uploadable } from "../utils/media";

defineProps<{ busy?: boolean; inputId: string }>();
const emit = defineEmits<{ files: [files: File[]] }>();
const over = ref(false);
const input = ref<HTMLInputElement | null>(null);

const choose = (files: ArrayLike<File> | null | undefined) => {
  const picked = uploadable(files);
  if (picked.length) emit("files", picked);
};
const onChange = (event: Event) => {
  const target = event.target as HTMLInputElement;
  choose(target.files);
  target.value = "";
};
</script>

<template>
  <div
    class="dropzone"
    :class="{ 'is-over': over }"
    @dragenter.prevent="over = true"
    @dragover.prevent="over = true"
    @dragleave.prevent="over = false"
    @drop.prevent="
      over = false;
      choose($event.dataTransfer?.files);
    "
  >
    <UIcon name="i-lucide-images" class="dropzone-icon" aria-hidden="true" />
    <p>Drop images or PDFs here, or</p>
    <label :for="inputId" class="sr-only">Choose files to upload</label>
    <input
      :id="inputId"
      ref="input"
      type="file"
      class="sr-only"
      multiple
      :accept="acceptedTypes"
      @change="onChange"
    />
    <UButton :loading="busy" icon="i-lucide-plus" @click="input?.click()"
      >Choose files</UButton
    >
    <p class="field-hint">JPEG, PNG, GIF, WebP, AVIF or PDF. SVG is not accepted.</p>
  </div>
</template>
