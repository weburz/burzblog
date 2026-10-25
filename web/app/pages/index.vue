<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import type { Post, PostList, PostStatus } from "../types";
import { errorMessage, request } from "../utils/api";
import { wordLabel } from "../utils/markdown";
import { editedLabel, exactTime } from "../utils/time";

useHead({ title: "Posts" });
const route = useRoute();
const router = useRouter();
const { state } = useCurrentSession();

type Filter = "" | PostStatus;
const filters: { value: Filter; label: string }[] = [
  { value: "", label: "All" },
  { value: "draft", label: "Drafts" },
  { value: "published", label: "Published" },
  { value: "archived", label: "Archived" },
];
const asFilter = (value: unknown): Filter =>
  filters.some((f) => f.value === value) ? (value as Filter) : "";

// The address holds the filter and search, so Back and reload keep them.
const status = ref<Filter>(asFilter(route.query.status));
const search = ref(typeof route.query.q === "string" ? route.query.q : "");
const list = ref<PostList | null>(null);
const loading = ref(true);
const error = ref("");
const creating = ref(false);

let sequence = 0;
const load = async () => {
  const ticket = ++sequence;
  loading.value = true;
  error.value = "";
  const query = new URLSearchParams();
  if (status.value) query.set("status", status.value);
  if (search.value.trim()) query.set("q", search.value.trim());
  try {
    const result = await request<PostList>(`/api/posts?${query}`);
    // A slower, older request must not overwrite a newer one.
    if (ticket === sequence) list.value = result;
  } catch (cause) {
    if (ticket === sequence) error.value = errorMessage(cause);
  } finally {
    if (ticket === sequence) loading.value = false;
  }
};

let debounce: ReturnType<typeof setTimeout> | undefined;
watch([status, search], ([s, q], [oldS]) => {
  void router.replace({
    query: { ...(s ? { status: s } : {}), ...(q.trim() ? { q: q.trim() } : {}) },
  });
  clearTimeout(debounce);
  if (s !== oldS) void load();
  else debounce = setTimeout(load, 200);
});
onMounted(load);

const total = computed(() => {
  const c = list.value?.counts;
  return c ? c.draft + c.published : 0;
});
const countFor = (value: Filter) => {
  const c = list.value?.counts;
  if (!c) return null;
  return value === "" ? c.draft + c.published : c[value];
};
const filtered = computed(() => Boolean(status.value || search.value.trim()));
const emptyText = computed(() => {
  const noun = {
    "": "posts",
    draft: "drafts",
    published: "published posts",
    archived: "archived posts",
  }[status.value];
  return search.value.trim()
    ? `No ${noun} match “${search.value.trim()}”.`
    : `No ${noun} yet.`;
});
const clearFilters = () => {
  status.value = "";
  search.value = "";
};

const created = (post: Post) => navigateTo(`/posts/${post.slug}`);

// "/" jumps to search and "n" starts a post, unless the writer is typing.
const shortcut = (event: KeyboardEvent) => {
  const target = event.target as HTMLElement | null;
  if (event.metaKey || event.ctrlKey || event.altKey) return;
  if (target?.closest("input, textarea, [contenteditable], [role=dialog]")) return;
  if (event.key === "/") {
    event.preventDefault();
    document.querySelector<HTMLInputElement>("#post-search")?.focus();
  } else if (event.key === "n") {
    event.preventDefault();
    creating.value = true;
  }
};
onMounted(() => window.addEventListener("keydown", shortcut));
onUnmounted(() => window.removeEventListener("keydown", shortcut));

const origin = typeof window === "undefined" ? "" : window.location.origin;
</script>

<template>
  <div class="content">
    <div class="page-head">
      <div>
        <h1 class="page-title">Posts</h1>
        <p v-if="list" class="muted">
          <template v-if="total === 0">Nothing written yet.</template>
          <template v-else
            >{{ list.counts.published }} published, {{ list.counts.draft }}
            {{ list.counts.draft === 1 ? "draft" : "drafts" }}</template
          >
        </p>
      </div>
      <UButton icon="i-lucide-plus" size="lg" @click="creating = true">New post</UButton>
    </div>

    <div class="list-controls">
      <div class="segment" role="group" aria-label="Show">
        <button
          v-for="f in filters"
          :key="f.value"
          type="button"
          class="segment-item"
          :aria-pressed="status === f.value"
          @click="status = f.value"
        >
          {{ f.label }}
          <span v-if="countFor(f.value) !== null" class="segment-count">{{
            countFor(f.value)
          }}</span>
        </button>
      </div>
      <UInput
        id="post-search"
        v-model="search"
        type="search"
        icon="i-lucide-search"
        placeholder="Search titles and slugs"
        aria-label="Search posts"
        class="list-search"
      >
        <template #trailing>
          <UKbd v-if="!search" value="/" class="max-sm:hidden" />
        </template>
      </UInput>
    </div>

    <p v-if="error" class="alert" role="alert">
      {{ error }}
      <UButton size="xs" color="neutral" variant="outline" class="ml-2" @click="load"
        >Try again</UButton
      >
    </p>

    <div v-if="list && list.items.length" aria-live="polite" class="sr-only">
      {{ list.items.length }} {{ list.items.length === 1 ? "post" : "posts" }} shown
    </div>

    <ul v-if="list && list.items.length" class="post-list" :aria-busy="loading">
      <li v-for="post in list.items" :key="post.slug">
        <NuxtLink :to="`/posts/${post.slug}`" class="post-row">
          <span class="post-main">
            <span class="post-title" :class="{ 'post-untitled': !post.title }">{{
              post.title || "Untitled"
            }}</span>
            <span class="post-sub">
              <span class="mono">{{ post.slug }}</span>
              <span v-if="post.description" class="post-description">
                {{ post.description }}</span
              >
            </span>
          </span>
          <StatusBadge :status="post.status" class="post-status" />
          <span class="post-words">{{ wordLabel(post.words) }}</span>
          <time
            class="post-edited"
            :datetime="post.updatedAt"
            :title="exactTime(post.updatedAt)"
            >{{ editedLabel(post.updatedAt) }}
            <span v-if="post.updatedBy && post.updatedBy !== state.username" class="muted"
              >by {{ post.updatedBy }}</span
            ></time
          >
        </NuxtLink>
      </li>
    </ul>

    <div v-else-if="list && filtered" class="empty-state">
      <p class="empty-title">{{ emptyText }}</p>
      <UButton color="neutral" variant="outline" @click="clearFilters"
        >Show all posts</UButton
      >
    </div>

    <div v-else-if="list" class="empty-state empty-first">
      <p class="empty-title">Your first post starts here.</p>
      <p class="muted">Write it in the browser, or from your own editor.</p>
      <div class="empty-options">
        <div class="empty-option">
          <UButton icon="i-lucide-plus" size="lg" @click="creating = true"
            >New post</UButton
          >
        </div>
        <div class="empty-option">
          <p class="field-hint">From a terminal:</p>
          <pre class="terminal"><code>burzblog login {{ origin }}
burzblog new "My first post"</code></pre>
        </div>
      </div>
    </div>

    <p v-else-if="loading" class="loading" role="status">Loading posts…</p>

    <p v-if="list && list.items.length" class="list-foot list-foot-keys muted">
      Press <UKbd value="n" /> for a new post, <UKbd value="/" /> to search,
      <UKbd value="?" /> for all shortcuts.
    </p>

    <NewPostDialog v-model:open="creating" @created="created" />
  </div>
</template>
