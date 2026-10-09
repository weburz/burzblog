<script setup lang="ts">
import { computed } from "vue";
import type { PostSummary } from "../types";
import { editedLabel, exactTime } from "../utils/time";

const props = defineProps<{
  post: PostSummary;
  opening: boolean;
  error: string | null;
  now: number;
}>();
const emit = defineEmits<{ open: []; dismiss: [] }>();
const basename = (path: string) => path.slice(path.lastIndexOf("/") + 1);
const directory = (path: string) => path.slice(0, path.lastIndexOf("/") + 1);
const unavailable = computed(() => props.post.status === "unavailable");
const status = computed(() =>
  props.post.status === "published" || props.post.status === "draft"
    ? props.post.status
    : "unavailable",
);
// Rows show the filename; the directory is added only outside posts/.
const shownPath = computed(() =>
  directory(props.post.path) === "posts/" ? basename(props.post.path) : props.post.path,
);
const edited = computed(() => editedLabel(props.post.modified, props.now));
</script>

<template>
  <li>
    <button
      class="post-row"
      :title="post.path"
      :aria-busy="opening ? 'true' : undefined"
      @click="emit('open')"
    >
      <span v-if="unavailable" class="post-title">{{ basename(post.path) }}</span>
      <span v-else-if="post.title" class="post-title" :title="post.title">{{
        post.title
      }}</span>
      <span v-else class="post-title post-untitled">Untitled</span>
      <span v-if="!unavailable" class="post-path">{{ shownPath }}</span>
      <time
        v-if="edited"
        class="post-edited"
        :datetime="post.modified"
        :title="`Changed on disk ${exactTime(post.modified)}`"
        >{{ edited }}</time
      >
      <span class="post-status"
        ><UIcon
          v-if="opening"
          name="i-lucide-loader-circle"
          class="size-4 animate-spin text-[var(--bb-text-3)]"
          aria-label="Opening" /><StatusBadge v-else :status="status"
      /></span>
      <span v-if="unavailable" class="post-note"
        >Could not be read. Open it to see why.</span
      >
    </button>
    <div v-if="error" class="row-error" role="alert">
      <p>{{ error }}</p>
      <div class="actions">
        <UButton color="neutral" variant="outline" size="sm" @click="emit('open')"
          >Try again</UButton
        >
        <UButton color="neutral" variant="ghost" size="sm" @click="emit('dismiss')"
          >Dismiss</UButton
        >
      </div>
    </div>
  </li>
</template>
