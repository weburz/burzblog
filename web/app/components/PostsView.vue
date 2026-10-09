<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import type { Article, PostList } from "../types";
import { ApiError, request, errorMessage } from "../utils/api";
import { withRetainedFocus } from "../utils/focus";
import { pageSize, type ListFilter, type ListSort, type ListState } from "../utils/route";

type Filter = ListFilter;
type Sort = ListSort;
const props = defineProps<{ initial: ListState; restoreScroll?: number | null }>();
const filters: { value: Filter; label: string }[] = [
  { value: "all", label: "All" },
  { value: "draft", label: "Drafts" },
  { value: "published", label: "Published" },
];
const sorts: { value: Sort; label: string }[] = [
  { value: "modified", label: "Recently changed" },
  { value: "path", label: "By path" },
];
const emit = defineEmits<{ open: [article: Article]; change: [list: ListState] }>();
const posts = ref<PostList>({ items: [], total: 0, files: 0 });
const offset = ref(props.initial.offset);
const search = ref(props.initial.q);
const filter = ref<Filter>(props.initial.filter);
const sort = ref<Sort>(props.initial.sort);
// Returning with browser Back puts the list where the writer left it, once.
let pendingScroll = props.restoreScroll ?? null;
const loaded = ref(false);
const creating = ref(false);
const error = ref("");
const errorCode = ref("");
const busy = ref(false);
const opening = ref<string | null>(null);
// A failed open is reported on its own row, where the writer is looking.
const rowError = ref<{ path: string; message: string } | null>(null);
// Relative "changed" labels are computed against one clock per load.
const now = ref(Date.now());
const term = computed(() => search.value.trim());
const filtering = computed(() => filter.value !== "all" || term.value !== "");
const range = computed(() =>
  posts.value.total > pageSize
    ? ` · showing ${offset.value + 1}–${offset.value + posts.value.items.length}`
    : "",
);
const summary = computed(() => {
  const { total, files } = posts.value;
  const counted = `${files} ${files === 1 ? "file" : "files"}`;
  return filtering.value ? `${total} of ${counted} match` : counted;
});
// Files that exist on disk but cannot appear here, so nothing vanishes silently.
const leftOut = computed(() => {
  const { unsupported_names: names = 0, unsafe_folders: folders = 0 } =
    posts.value.left_out ?? {};
  const parts = [];
  if (names)
    parts.push(
      `${names} Markdown ${names === 1 ? "file has a name" : "files have names"} BurzBlog cannot use (spaces, accents or a leading _ or -) and ${names === 1 ? "is" : "are"} not listed. Rename ${names === 1 ? "it" : "them"} with letters, numbers, dots, hyphens and underscores.`,
    );
  if (folders)
    parts.push(
      `${folders} ${folders === 1 ? "folder is" : "folders are"} skipped because other users can write to ${folders === 1 ? "it" : "them"}; ask the operator to make ${folders === 1 ? "it" : "them"} private (chmod go-w).`,
    );
  return parts.join(" ");
});
// Filtering and search run on the server over every file, so a response can
// arrive after a newer request; only the latest one is shown.
let latest = 0;
const refresh = (next = offset.value) =>
  withRetainedFocus(async () => {
    const ticket = ++latest;
    // A new page starts at its first row.
    const paging = loaded.value && next !== offset.value;
    busy.value = true;
    error.value = "";
    const query = new URLSearchParams({
      offset: String(next),
      limit: String(pageSize),
      sort: sort.value,
    });
    if (filter.value !== "all") query.set("status", filter.value);
    if (term.value) query.set("q", term.value);
    try {
      const list = await request<PostList>(`/api/posts?${query}`);
      if (ticket !== latest) return;
      posts.value = list;
      offset.value = next;
      now.value = Date.now();
      emit("change", {
        filter: filter.value,
        q: term.value,
        sort: sort.value,
        offset: next,
      });
    } catch (cause) {
      if (ticket !== latest) return;
      error.value = errorMessage(cause);
      errorCode.value = cause instanceof ApiError ? cause.code : "";
    } finally {
      if (ticket === latest) {
        busy.value = false;
        loaded.value = true;
      }
    }
    // Only once the rows (not the loading skeleton) are on the page.
    if (ticket === latest && paging && !error.value) {
      await nextTick();
      window.scrollTo(0, 0);
    }
    if (ticket === latest && pendingScroll !== null && !error.value) {
      const top = pendingScroll;
      pendingScroll = null;
      await nextTick();
      window.scrollTo(0, top);
    }
  });
watch([filter, sort], () => refresh(0));
let typing: ReturnType<typeof setTimeout> | undefined;
watch(term, () => {
  clearTimeout(typing);
  typing = setTimeout(() => refresh(0), 250);
});
const open = async (path: string) => {
  if (opening.value) return;
  opening.value = path;
  rowError.value = null;
  try {
    emit("open", await request<Article>(`/api/post?path=${encodeURIComponent(path)}`));
  } catch (cause) {
    rowError.value = {
      path,
      message:
        cause instanceof ApiError && cause.code === "not_found"
          ? "This file is no longer on disk. It may have been moved or deleted outside BurzBlog; refresh the list."
          : errorMessage(cause),
    };
  } finally {
    opening.value = null;
  }
};
const restored = () => {
  if (errorCode.value === "authentication") void refresh();
  if (rowError.value) rowError.value = null;
};
const clearFilter = () => {
  filter.value = "all";
  search.value = "";
};
const titleElement = ref<HTMLElement | null>(null);
onMounted(() => {
  titleElement.value?.focus();
  void refresh();
  window.addEventListener("burzblog:session-restored", restored);
});
onUnmounted(() => {
  clearTimeout(typing);
  window.removeEventListener("burzblog:session-restored", restored);
});
</script>

<template>
  <section class="panel">
    <div class="toolbar mb-6">
      <h1 ref="titleElement" tabindex="-1" class="page-title">Posts</h1>
      <UButton color="primary" icon="i-lucide-plus" @click="creating = true"
        >New post</UButton
      >
    </div>
    <template v-if="!loaded || posts.files > 0 || filtering">
      <div class="list-controls">
        <div class="segment" role="group" aria-label="Filter by status">
          <UButton
            v-for="option in filters"
            :key="option.value"
            color="neutral"
            :variant="filter === option.value ? 'soft' : 'ghost'"
            :aria-pressed="filter === option.value"
            @click="filter = option.value"
            >{{ option.label }}</UButton
          >
        </div>
        <div class="list-search">
          <UInput
            v-model="search"
            icon="i-lucide-search"
            class="w-full"
            aria-label="Search titles and filenames"
            placeholder="Search titles and filenames…"
          >
            <template v-if="search" #trailing>
              <UButton
                color="neutral"
                variant="link"
                size="sm"
                icon="i-lucide-x"
                aria-label="Clear search"
                @click="search = ''"
              />
            </template>
          </UInput>
          <UButton
            color="neutral"
            variant="ghost"
            icon="i-lucide-refresh-cw"
            aria-label="Refresh"
            :loading="busy"
            @click="refresh()"
          />
        </div>
      </div>
      <div class="list-head">
        <p class="meta" role="status">
          <template v-if="loaded">{{ summary }}{{ range }}</template>
        </p>
        <USelect
          v-model="sort"
          :items="sorts"
          value-key="value"
          size="sm"
          variant="ghost"
          aria-label="Sort posts"
          class="list-sort"
        />
      </div>
    </template>
    <p v-if="leftOut" class="alert alert-warn" role="note">{{ leftOut }}</p>
    <div v-if="error" class="alert" role="alert">
      <p>{{ error }}</p>
      <UButton class="mt-2" color="neutral" variant="outline" @click="refresh()"
        >Try again</UButton
      >
    </div>
    <div v-if="!loaded" :aria-busy="true" class="post-list">
      <p class="sr-only" role="status">Loading posts…</p>
      <div v-for="row in 4" :key="row" class="py-4">
        <USkeleton class="h-5 w-3/5" />
        <USkeleton class="mt-2 h-4 w-[35%]" />
      </div>
    </div>
    <div v-else-if="!posts.files && !filtering && !error" class="empty-state">
      <UIcon name="i-lucide-feather" class="size-8 text-[var(--bb-text-3)]" />
      <h2 class="section-title">Nothing here yet</h2>
      <p class="muted">Start a draft, or drop a Markdown file into posts/.</p>
    </div>
    <div v-else-if="!posts.items.length && !error" class="empty-state">
      <h2 class="section-title">No posts match.</h2>
      <UButton color="secondary" variant="link" @click="clearFilter"
        >Clear filter and search</UButton
      >
    </div>
    <ul v-else-if="posts.items.length" aria-label="Posts" class="post-list">
      <PostRow
        v-for="post in posts.items"
        :key="post.path"
        :post="post"
        :now="now"
        :opening="opening === post.path"
        :error="rowError?.path === post.path ? rowError.message : null"
        @open="open(post.path)"
        @dismiss="rowError = null"
      />
    </ul>
    <div v-if="posts.total > pageSize" class="toolbar mt-6">
      <p class="meta">Showing {{ offset + 1 }}–{{ offset + posts.items.length }}</p>
      <div class="actions">
        <UButton
          v-if="offset > 0"
          color="neutral"
          variant="outline"
          icon="i-lucide-chevron-left"
          @click="refresh(offset - pageSize)"
          >Previous</UButton
        ><UButton
          v-if="offset + pageSize < posts.total"
          color="neutral"
          variant="outline"
          trailing-icon="i-lucide-chevron-right"
          @click="refresh(offset + pageSize)"
          >Next</UButton
        >
      </div>
    </div>
    <NewPostDialog v-model:open="creating" @created="emit('open', $event)" />
  </section>
</template>
