<script setup lang="ts">
import { watch } from "vue";
import type { MediaItem } from "../types";
import { detailLabel, isImage } from "../utils/media";

// Choose an uploaded image, or upload a new one, for the editor and the cover.
const open = defineModel<boolean>("open", { default: false });
const emit = defineEmits<{ pick: [item: MediaItem] }>();
const media = useUploadedMedia();

watch(open, (value) => {
  if (value) void media.load();
});

const add = async (files: File[]) => {
  const done = await media.upload(files);
  // One new image is almost certainly the one to use.
  const [only] = done;
  if (done.length === 1 && only && isImage(only) && !media.failures.value.length)
    pick(only);
};
const pick = (item: MediaItem) => {
  emit("pick", item);
  open.value = false;
};
</script>

<template>
  <UModal
    v-model:open="open"
    title="Choose an image"
    description="Pick a file you uploaded, or upload a new one."
    :ui="{ content: 'max-w-2xl' }"
  >
    <template #body>
      <div class="flex flex-col gap-5">
        <MediaDropzone
          input-id="picker-upload"
          :busy="media.uploading.value"
          @files="add"
        />
        <p
          v-for="line in media.failures.value"
          :key="line"
          class="field-error"
          role="alert"
        >
          {{ line }}
        </p>
        <p v-if="media.error.value" class="field-error" role="alert">
          {{ media.error.value }}
        </p>
        <ul
          v-if="media.items.value.some(isImage)"
          class="media-grid"
          aria-label="Uploaded images"
        >
          <li v-for="item in media.items.value.filter(isImage)" :key="item.id">
            <button type="button" class="media-pick" @click="pick(item)">
              <img :src="item.url" :alt="item.alt" loading="lazy" />
              <span class="media-name">{{ item.originalName || item.name }}</span>
              <span class="field-hint">{{ detailLabel(item) }}</span>
            </button>
          </li>
        </ul>
        <p v-else-if="!media.loading.value && !media.error.value" class="field-hint">
          Nothing uploaded yet.
        </p>
        <UButton
          v-if="media.items.value.length < media.total.value"
          color="neutral"
          variant="outline"
          :loading="media.loading.value"
          @click="media.load(true)"
          >Show more</UButton
        >
      </div>
    </template>
    <template #footer>
      <div class="flex w-full justify-end">
        <UButton color="neutral" variant="ghost" @click="open = false">Cancel</UButton>
      </div>
    </template>
  </UModal>
</template>
