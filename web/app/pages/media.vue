<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from "vue";
import type { MediaItem } from "../types";
import { copyText } from "../utils/clipboard";
import { detailLabel, isImage, markdownFor, usageLabel } from "../utils/media";
import { exactTime } from "../utils/time";

useHead({ title: "Media" });
const toast = useToast();
const media = useUploadedMedia();
const { isAdmin } = useCurrentSession();
const deleting = ref<MediaItem | null>(null);
const deleteOpen = ref(false);
const deleteError = ref("");
const inUse = ref(false);
// The cards of the last batch, which are asked for alt text.
const fresh = ref<number[]>([]);

onMounted(() => media.load());

const add = async (files: File[]) => {
  const done = await media.upload(files);
  fresh.value = done.map((item) => item.id);
  if (!done.length) return;
  toast.add({
    title: done.length === 1 ? "Uploaded" : `Uploaded ${done.length} files`,
    color: "success",
  });
  // Move to the first new image without a description, so it can be typed now.
  const missing = done.find((item) => isImage(item) && !item.alt);
  if (!missing) return;
  await nextTick();
  document.getElementById(`alt-${missing.id}`)?.focus();
};

const missingAlt = () =>
  media.items.value.filter(
    (item) => fresh.value.includes(item.id) && isImage(item) && !item.alt,
  ).length;

// What the browser would not copy, shown in a dialog that stays open.
const manual = ref<{ what: string; text: string } | null>(null);
const manualOpen = ref(false);
const copy = async (text: string, what: string) => {
  if (await copyText(text)) {
    toast.add({ title: `${what} copied`, color: "success" });
    return;
  }
  manual.value = { what, text };
  manualOpen.value = true;
};

const askDelete = (item: MediaItem) => {
  deleting.value = item;
  deleteError.value = "";
  inUse.value = false;
  deleteOpen.value = true;
};
const deleteBody = computed(() => {
  if (deleteError.value) return deleteError.value;
  const item = deleting.value;
  if (!item) return "";
  const broken = item.usedIn ? " Those posts will show a broken image." : "";
  return `Delete ${item.originalName || item.name}? Its public address stops working. ${usageLabel(item)}.${broken}`;
});
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

    <MediaDropzone input-id="media-upload" :busy="media.uploading.value" @files="add" />

    <p v-if="missingAlt()" class="media-prompt" role="status">
      Add alt text to the {{ missingAlt() === 1 ? "new image" : "new images" }} below. It
      describes them for people who cannot see them and is used when you copy the
      Markdown.
    </p>
    <p v-for="line in media.failures.value" :key="line" class="field-error" role="alert">
      {{ line }}
    </p>
    <p v-if="media.error.value" class="alert" role="alert">{{ media.error.value }}</p>

    <MediaFilters
      v-model:query="media.query.value"
      v-model:kind="media.kind.value"
      id-prefix="media"
      show-kind
      @search="media.typeAhead()"
      @kind="media.refine()"
    />

    <ul v-if="media.items.value.length" class="media-grid" aria-label="Uploaded files">
      <li
        v-for="item in media.items.value"
        :key="item.id"
        class="media-card"
        :class="{ 'is-new': fresh.includes(item.id) }"
      >
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
        <span class="field-hint">{{ usageLabel(item) }}</span>
        <MediaAltField
          :input-id="`alt-${item.id}`"
          :alt="item.alt"
          :label="item.originalName || item.name"
          :save="(alt) => media.saveAlt(item, alt)"
        />
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
    <p
      v-else-if="!media.loading.value && !media.error.value"
      class="field-hint"
      role="status"
    >
      {{
        media.query.value.trim() || media.kind.value
          ? "No files match."
          : "Nothing uploaded yet. Uploaded files are served publicly at a permanent address, so posts can use them."
      }}
    </p>
    <UButton
      v-if="media.items.value.length < media.total.value"
      color="neutral"
      variant="outline"
      :loading="media.loading.value"
      @click="media.load(true)"
      >Show more</UButton
    >

    <CopyFallbackDialog
      v-model:open="manualOpen"
      :title="`Copy the ${(manual?.what ?? 'text').toLowerCase()}`"
      :value="manual?.text ?? ''"
      :label="`the ${(manual?.what ?? 'text').toLowerCase()}`"
    />

    <ConfirmDialog
      v-model:open="deleteOpen"
      :title="inUse ? 'Delete anyway?' : 'Delete this file?'"
      :body="deleteBody"
      :confirm-label="inUse ? 'Delete anyway' : 'Delete'"
      confirm-color="error"
      @confirm="confirmDelete"
    />
  </div>
</template>
