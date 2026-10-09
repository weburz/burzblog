<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import type { Article, PostList } from "../types";
import { ApiError, request, errorMessage } from "../utils/api";
import { withRetainedFocus } from "../utils/focus";
import { slugify } from "../utils/posts";

type Filter = "all" | "draft" | "published";
const pageSize = 100;
const filters: { value: Filter; label: string }[] = [
  { value: "all", label: "All" },
  { value: "draft", label: "Drafts" },
  { value: "published", label: "Published" },
];
const emit = defineEmits<{ open: [article: Article] }>();
const posts = ref<PostList>({ items: [], total: 0 });
const offset = ref(0);
const search = ref("");
const filter = ref<Filter>("all");
const loaded = ref(false);
const creating = ref(false);
const title = ref("");
const slug = ref("");
const slugTouched = ref(false);
const error = ref("");
const errorCode = ref("");
const createError = ref("");
const busy = ref(false);
const opening = ref<string | null>(null);
// A failed open is reported on its own row, where the writer is looking.
const rowError = ref<{ path: string; message: string } | null>(null);
const filtering = computed(() => filter.value !== "all" || search.value.trim() !== "");
const filtered = computed(() =>
  posts.value.items.filter(
    (post) =>
      (filter.value === "all" || post.status === filter.value) &&
      post.title.toLowerCase().includes(search.value.trim().toLowerCase()),
  ),
);
const rowStatus = (status: string) =>
  status === "published" || status === "draft" ? status : "unavailable";
const basename = (path: string) => path.slice(path.lastIndexOf("/") + 1);
const directory = (path: string) => path.slice(0, path.lastIndexOf("/") + 1);
// Rows show the filename; the directory is added only outside posts/.
const rowPath = (path: string) => (directory(path) === "posts/" ? basename(path) : path);
const newPath = computed(() => `posts/${slug.value.trim() || "…"}.md`);
const refresh = (next = offset.value) =>
  withRetainedFocus(async () => {
    busy.value = true;
    error.value = "";
    try {
      posts.value = await request<PostList>(
        `/api/posts?offset=${next}&limit=${pageSize}`,
      );
      offset.value = next;
    } catch (cause) {
      error.value = errorMessage(cause);
      errorCode.value = cause instanceof ApiError ? cause.code : "";
    } finally {
      busy.value = false;
      loaded.value = true;
    }
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
onMounted(() => window.addEventListener("burzblog:session-restored", restored));
onUnmounted(() => window.removeEventListener("burzblog:session-restored", restored));
const startCreate = () => {
  title.value = "";
  slug.value = "";
  slugTouched.value = false;
  createError.value = "";
  creating.value = true;
};
const clearFilter = () => {
  filter.value = "all";
  search.value = "";
};
watch(title, (value) => {
  if (!slugTouched.value) slug.value = slugify(value);
});
const create = () =>
  withRetainedFocus(async () => {
    createError.value = "";
    if (!title.value.trim()) {
      createError.value = "Give the post a title.";
      return;
    }
    busy.value = true;
    try {
      const article = await request<Article>("/api/posts", "POST", {
        title: title.value,
        slug: slug.value,
      });
      creating.value = false;
      emit("open", article);
    } catch (cause) {
      createError.value =
        cause instanceof ApiError && cause.code === "exists"
          ? `${newPath.value} already exists. Choose a different filename.`
          : errorMessage(cause);
    } finally {
      busy.value = false;
    }
  });
const titleElement = ref<HTMLElement | null>(null);
onMounted(() => {
  titleElement.value?.focus();
  void refresh();
});
</script>

<template>
  <section class="panel">
    <div class="toolbar mb-6">
      <h1 ref="titleElement" tabindex="-1" class="page-title">Posts</h1>
      <UButton color="primary" icon="i-lucide-plus" @click="startCreate"
        >New post</UButton
      >
    </div>
    <div class="toolbar">
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
      <div class="actions">
        <UInput
          v-model="search"
          icon="i-lucide-search"
          aria-label="Filter this page of posts"
          placeholder="Filter titles…"
        >
          <template v-if="search" #trailing>
            <UButton
              color="neutral"
              variant="link"
              size="sm"
              icon="i-lucide-x"
              aria-label="Clear filter"
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
    <div v-else-if="!posts.items.length && !error" class="empty-state">
      <h2 class="section-title">Nothing here yet</h2>
      <p class="muted">Start a draft, or drop a Markdown file into posts/.</p>
      <UButton color="neutral" variant="outline" @click="startCreate"
        >Start a draft</UButton
      >
    </div>
    <div v-else-if="posts.items.length && !filtered.length" class="empty-state">
      <h2 class="section-title">No posts match.</h2>
      <UButton color="secondary" variant="link" @click="clearFilter"
        >Clear filter</UButton
      >
    </div>
    <ul v-else aria-label="Posts" class="post-list">
      <li v-for="post in filtered" :key="post.path">
        <button
          class="post-row"
          :title="post.path"
          :aria-busy="opening === post.path ? 'true' : undefined"
          @click="open(post.path)"
        >
          <span class="post-title" :title="post.title">{{
            post.status === "unavailable" ? basename(post.path) : post.title
          }}</span>
          <span v-if="post.status !== 'unavailable'" class="post-path">{{
            rowPath(post.path)
          }}</span>
          <span class="post-status"
            ><UIcon
              v-if="opening === post.path"
              name="i-lucide-loader-circle"
              class="size-4 animate-spin text-[var(--bb-text-3)]"
              aria-label="Opening" /><StatusBadge v-else :status="rowStatus(post.status)"
          /></span>
          <span v-if="post.status === 'unavailable'" class="post-note"
            >Could not be read. Check its frontmatter on disk.</span
          >
        </button>
        <div v-if="rowError?.path === post.path" class="row-error" role="alert">
          <p>{{ rowError.message }}</p>
          <div class="actions">
            <UButton color="neutral" variant="outline" size="sm" @click="open(post.path)"
              >Try again</UButton
            >
            <UButton color="neutral" variant="ghost" size="sm" @click="rowError = null"
              >Dismiss</UButton
            >
          </div>
        </div>
      </li>
    </ul>
    <div class="toolbar mt-6">
      <div class="meta-row">
        <p v-if="loaded" class="meta">
          {{ posts.total }} files on disk
          <template v-if="posts.total > pageSize">
            · Showing {{ offset + 1 }}–{{ offset + posts.items.length }} of
            {{ posts.total }}</template
          >
        </p>
        <p class="meta" role="status">
          <template v-if="filtering && loaded"
            >{{ filtered.length }} of {{ posts.items.length }} posts match</template
          >
        </p>
      </div>
      <div class="actions">
        <UButton
          v-if="offset > 0"
          color="neutral"
          variant="outline"
          @click="refresh(offset - pageSize)"
          >Previous</UButton
        ><UButton
          v-if="offset + pageSize < posts.total"
          color="neutral"
          variant="outline"
          @click="refresh(offset + pageSize)"
          >Next</UButton
        >
      </div>
    </div>
    <UModal
      v-model:open="creating"
      title="New post"
      description="Creates an ordinary Markdown draft in posts/."
    >
      <template #body>
        <form id="new-post-form" @submit.prevent="create">
          <div class="field">
            <label for="new-title">Title</label
            ><UInput
              id="new-title"
              v-model="title"
              required
              autofocus
              maxlength="500"
              class="w-full"
              size="lg"
            />
          </div>
          <div class="field !mb-0">
            <label for="new-slug">Filename / slug</label
            ><UInput
              id="new-slug"
              v-model="slug"
              required
              maxlength="128"
              pattern="[a-z0-9]+(?:-[a-z0-9]+)*"
              class="w-full font-mono"
              placeholder="my-first-post"
              aria-describedby="slug-hint"
              @update:model-value="slugTouched = true"
            >
              <template #trailing><span class="meta mono">.md</span></template>
            </UInput>
            <p id="slug-hint" class="meta">Lowercase letters, numbers and hyphens.</p>
            <p class="meta mono">Creates {{ newPath }}</p>
          </div>
          <p v-if="createError" class="alert" role="alert">{{ createError }}</p>
        </form>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" @click="creating = false"
            >Cancel</UButton
          >
          <UButton type="submit" form="new-post-form" color="primary" :loading="busy"
            >Create draft</UButton
          >
        </div>
      </template>
    </UModal>
  </section>
</template>
