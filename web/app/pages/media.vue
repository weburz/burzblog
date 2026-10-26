<script setup lang="ts">
import { onMounted, ref } from "vue";
import type { MediaItem } from "../types";
import { detailLabel, isImage, markdownFor } from "../utils/media";
import { exactTime } from "../utils/time";

useHead({ title: "Media" });
const toast = useToast();
const media = useUploadedMedia();
const { isAdmin } = useCurrentSession();
const alt = ref("");
const deleting = ref<MediaItem | null>(null);
const deleteOpen = ref(false);
const deleteError = ref("");
const inUse = ref(false);

onMounted(() => media.load());

const add = async (files: File[]) => {
  const done = await media.upload(files, alt.value.trim());
  if (done.length)
    toast.add({
      title: done.length === 1 ? "Uploaded" : `Uploaded ${done.length} files`,
      color: "success",
    });
};

const copy = async (text: string, what: string) => {
  try {
    await navigator.clipboard.writeText(text);
    toast.add({ title: `${what} copied`, color: "success" });
  } catch {
    toast.add({
      title: "Copying was blocked",
      description: "Your browser did not allow it. Select the address and copy it.",
      color: "warning",
    });
  }
};

const askDelete = (item: MediaItem) => {
  deleting.value = item;
  deleteError.value = "";
  inUse.value = false;
  deleteOpen.value = true;
};
const confirmDelete = async () => {
  if (!deleting.value) return;
  const result = await media.remove(deleting.value, inUse.value);
  if (!result.message) {
    deleteOpen.value = false;
    toast.add({ title: "Deleted", color: "success" });
    return;
  }
  // The posts that use it are named; a second confirm deletes it anyway.
  deleteError.value = result.message;
  inUse.value = result.inUse;
};
</script>

<template>
  <div class="content media">
    <div class="page-head">
      <h1 class="page-title">Media</h1>
    </div>

    <div class="field media-alt">
      <label for="media-alt">Alt text for new uploads (optional)</label>
      <UInput
        id="media-alt"
        v-model="alt"
        class="w-full"
        maxlength="500"
        aria-describedby="media-alt-hint"
      />
      <p id="media-alt-hint" class="field-hint">
        Describes the image for people who cannot see it. It is saved with the file and
        used when you copy the Markdown.
      </p>
    </div>
    <MediaDropzone input-id="media-upload" :busy="media.uploading.value" @files="add" />

    <p v-for="line in media.failures.value" :key="line" class="field-error" role="alert">
      {{ line }}
    </p>
    <p v-if="media.error.value" class="alert" role="alert">{{ media.error.value }}</p>

    <ul v-if="media.items.value.length" class="media-grid" aria-label="Uploaded files">
      <li v-for="item in media.items.value" :key="item.id" class="media-card">
        <img
          v-if="isImage(item)"
          :src="item.url"
          :alt="item.alt"
          loading="lazy"
          class="media-thumb"
        />
        <div v-else class="media-thumb media-file" aria-hidden="true">PDF</div>
        <span class="media-name">{{ item.originalName || item.name }}</span>
        <span class="field-hint"
          >{{ detailLabel(item) }} ·
          <time :datetime="item.createdAt" :title="exactTime(item.createdAt)">{{
            item.createdAt.slice(0, 10)
          }}</time></span
        >
        <div class="media-actions">
          <UButton
            size="sm"
            color="neutral"
            variant="outline"
            icon="i-lucide-copy"
            :aria-label="`Copy address of ${item.originalName || item.name}`"
            @click="copy(item.url, 'Address')"
            >URL</UButton
          >
          <UButton
            size="sm"
            color="neutral"
            variant="outline"
            icon="i-lucide-copy"
            :aria-label="`Copy Markdown for ${item.originalName || item.name}`"
            @click="copy(markdownFor(item), 'Markdown')"
            >Markdown</UButton
          >
          <UButton
            v-if="isAdmin"
            size="sm"
            color="error"
            variant="ghost"
            icon="i-lucide-trash-2"
            :aria-label="`Delete ${item.originalName || item.name}`"
            @click="askDelete(item)"
          />
        </div>
      </li>
    </ul>
    <p v-else-if="!media.loading.value && !media.error.value" class="field-hint">
      Nothing uploaded yet. Uploaded files are served publicly at a permanent address, so
      posts can use them.
    </p>
    <UButton
      v-if="media.items.value.length < media.total.value"
      color="neutral"
      variant="outline"
      :loading="media.loading.value"
      @click="media.load(true)"
      >Show more</UButton
    >

    <ConfirmDialog
      v-model:open="deleteOpen"
      :title="inUse ? 'Delete anyway?' : 'Delete this file?'"
      :body="
        deleteError ||
        'The file and its public address stop working. Posts that link to it will show a broken image.'
      "
      :confirm-label="inUse ? 'Delete anyway' : 'Delete'"
      confirm-color="error"
      @confirm="confirmDelete"
    />
  </div>
</template>
