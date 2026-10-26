<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import type { Post, PostList, PostStatus, PostSummary, TagCount } from "../types";
import { errorMessage, request } from "../utils/api";
import { wordLabel } from "../utils/markdown";
import {
  editedLabel,
  exactTime,
  isScheduled,
  publishedLabel,
  publishesLabel,
} from "../utils/time";

useHead({ title: "Posts" });
const route = useRoute();
const router = useRouter();
const { state } = useCurrentSession();
const { enabled: shortcutsEnabled } = useEnabledShortcuts();

type Filter = "" | PostStatus | "scheduled";
const filters: { value: Filter; label: string }[] = [
  { value: "", label: "All" },
  { value: "draft", label: "Drafts" },
  { value: "scheduled", label: "Scheduled" },
  { value: "published", label: "Published" },
  { value: "archived", label: "Archived" },
];
const asFilter = (value: unknown): Filter =>
  filters.some((f) => f.value === value) ? (value as Filter) : "";

type Sort = "updated" | "published" | "title" | "created";
const sorts: { value: Sort; label: string }[] = [
  { value: "updated", label: "Recently edited" },
  { value: "published", label: "Publish date" },
  { value: "title", label: "Title A–Z" },
  { value: "created", label: "Newest created" },
];
const asSort = (value: unknown): Sort =>
  sorts.find((s) => s.value === value)?.value ?? "updated";

// The address holds the filter, search and sort, so Back and reload keep them.
const status = ref<Filter>(asFilter(route.query.status));
const sort = ref<Sort>(asSort(route.query.sort));
const search = ref(typeof route.query.q === "string" ? route.query.q : "");
const tag = ref(typeof route.query.tag === "string" ? route.query.tag : "");
// "*" stands for no tag: a select cannot hold an empty value, and no tag is "*".
const anyTag = "*";
const tags = ref<TagCount[]>([]);
const list = ref<PostList | null>(null);
const loading = ref(true);
const loadingMore = ref(false);
const error = ref("");
const creating = ref(false);

// The server sends one page at a time, so a big blog opens fast; "Show more"
// asks for the next one.
const pageSize = 100;
let sequence = 0;
const load = async (more = false) => {
  const ticket = ++sequence;
  loading.value = !more;
  loadingMore.value = more;
  error.value = "";
  const query = new URLSearchParams({ limit: String(pageSize) });
  if (more && list.value) query.set("offset", String(list.value.items.length));
  if (status.value) query.set("status", status.value);
  if (search.value.trim()) query.set("q", search.value.trim());
  if (tag.value) query.set("tag", tag.value);
  if (sort.value !== "updated") query.set("sort", sort.value);
  try {
    const result = await request<PostList>(`/api/posts?${query}`);
    // A slower, older request must not overwrite a newer one.
    if (ticket === sequence) list.value = more ? appendPage(list.value, result) : result;
  } catch (cause) {
    if (ticket === sequence) error.value = errorMessage(cause);
  } finally {
    if (ticket === sequence) {
      loading.value = false;
      loadingMore.value = false;
    }
  }
};
// A post edited since the first page can shift the next one; skip repeats.
const appendPage = (current: PostList | null, next: PostList): PostList => {
  const seen = new Set(current?.items.map((p) => p.slug));
  return {
    ...next,
    items: [...(current?.items ?? []), ...next.items.filter((p) => !seen.has(p.slug))],
  };
};

// Tags only feed the filter, so a failure here leaves the list working.
const loadTags = async () => {
  try {
    tags.value = (await request<{ items: TagCount[] }>("/api/tags")).items;
  } catch {
    tags.value = [];
  }
};
const tagItems = computed(() => {
  const known = tags.value.map((t) => ({ value: t.tag, label: `${t.tag} (${t.count})` }));
  // A tag from the address that no post has still shows as the active filter.
  if (tag.value && !tags.value.some((t) => t.tag === tag.value))
    known.unshift({ value: tag.value, label: tag.value });
  return [{ value: anyTag, label: "All tags" }, ...known];
});

// The command palette's "Go to" entries change the filter from outside.
watch(
  () => route.query.status,
  (value) => (status.value = asFilter(value)),
);

let debounce: ReturnType<typeof setTimeout> | undefined;
watch([status, search, sort, tag], ([s, q, o, t], [oldS, , oldO, oldT]) => {
  void router.replace({
    query: {
      ...(s ? { status: s } : {}),
      ...(q.trim() ? { q: q.trim() } : {}),
      ...(o !== "updated" ? { sort: o } : {}),
      ...(t ? { tag: t } : {}),
    },
  });
  clearTimeout(debounce);
  if (s !== oldS || o !== oldO || t !== oldT) void load();
  else debounce = setTimeout(() => void load(), 200);
});
onMounted(() => {
  void load();
  void loadTags();
});

const total = computed(() => {
  const c = list.value?.counts;
  return c ? c.draft + c.published : 0;
});
const countFor = (value: Filter) => {
  const c = list.value?.counts;
  if (!c) return null;
  return value === "" ? c.draft + c.published : c[value];
};
const filtered = computed(() =>
  Boolean(status.value || search.value.trim() || tag.value),
);
const emptyText = computed(() => {
  const noun = {
    "": "posts",
    draft: "drafts",
    scheduled: "scheduled posts",
    published: "published posts",
    archived: "archived posts",
  }[status.value];
  const tagged = tag.value ? ` tagged “${tag.value}”` : "";
  return search.value.trim()
    ? `No ${noun}${tagged} match “${search.value.trim()}”.`
    : tag.value
      ? `No ${noun}${tagged}.`
      : `No ${noun} yet.`;
});
// A brand-new blog has nothing to filter, so the controls would only be noise.
const fresh = computed(() => {
  const c = list.value?.counts;
  return Boolean(
    c && !filtered.value && !list.value?.items.length && !c.archived && !c.scheduled,
  );
});
const clearFilters = () => {
  status.value = "";
  search.value = "";
  tag.value = "";
};

// One secondary line per row: published posts show when they went out,
// scheduled ones when they will, everything else when it was last edited.
const isPublished = (post: PostSummary) =>
  post.status === "published" && Boolean(post.publishedOn);
const rowDate = (post: PostSummary) =>
  isPublished(post)
    ? publishedLabel(post.publishedOn as string)
    : isScheduled(post)
      ? publishesLabel(post.publishAt as string)
      : editedLabel(post.updatedAt);

const created = (post: Post) => navigateTo(`/posts/${post.slug}`);

// "/" jumps to search and "n" starts a post, unless the writer is typing.
const shortcut = (event: KeyboardEvent) => {
  const target = event.target as HTMLElement | null;
  if (!shortcutsEnabled.value || event.metaKey || event.ctrlKey || event.altKey) return;
  if (
    target?.closest(
      "input, textarea, [contenteditable], [role=dialog], [role=combobox], [role=listbox]",
    )
  )
    return;
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
        <p v-if="list && !fresh" class="muted">
          <template v-if="total === 0">Nothing written yet.</template>
          <template v-else
            >{{ list.counts.published }} published, {{ list.counts.draft }}
            {{ list.counts.draft === 1 ? "draft" : "drafts"
            }}<template v-if="list.counts.scheduled">
              ({{ list.counts.scheduled }} scheduled)</template
            ></template
          >
        </p>
      </div>
      <UButton v-if="!fresh" icon="i-lucide-plus" size="lg" @click="creating = true"
        >New post</UButton
      >
    </div>

    <div v-if="!fresh" class="list-controls">
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
      <div class="list-find">
        <USelect
          v-if="tagItems.length > 1"
          :model-value="tag || anyTag"
          :items="tagItems"
          value-key="value"
          label-key="label"
          icon="i-lucide-tag"
          aria-label="Filter by tag"
          class="list-tag-select"
          @update:model-value="(v: string) => (tag = v === anyTag ? '' : v)"
        />
        <USelect
          v-model="sort"
          :items="sorts"
          value-key="value"
          label-key="label"
          icon="i-lucide-arrow-down-up"
          aria-label="Sort posts"
          class="list-sort-select"
        />
        <UInput
          id="post-search"
          v-model="search"
          type="search"
          icon="i-lucide-search"
          placeholder="Search posts"
          aria-label="Search posts"
          class="list-search"
        >
          <template #trailing>
            <!-- Always rendered: an empty slot falls back to a second magnifier. -->
            <UKbd value="/" class="max-sm:hidden" :class="{ invisible: search }" />
          </template>
        </UInput>
      </div>
    </div>

    <p v-if="error" class="alert" role="alert">
      {{ error }}
      <UButton size="xs" color="neutral" variant="outline" class="ml-2" @click="load()"
        >Try again</UButton
      >
    </p>

    <div aria-live="polite" class="sr-only">
      <template v-if="list && list.items.length"
        >{{ list.items.length }}
        {{ list.items.length === 1 ? "post" : "posts" }} shown</template
      >
      <template v-else-if="list">{{ emptyText }}</template>
    </div>

    <ul v-if="list && list.items.length" class="post-list" :aria-busy="loading">
      <li v-for="post in list.items" :key="post.slug">
        <NuxtLink :to="`/posts/${post.slug}`" class="post-row">
          <span class="post-main">
            <span class="post-title" :class="{ 'post-untitled': !post.title }">{{
              post.title || "Untitled"
            }}</span>
            <span class="post-sub">
              <span v-if="post.featured" class="post-featured"
                ><UIcon
                  name="i-lucide-star"
                  class="size-3.5"
                  aria-hidden="true"
                />Featured</span
              >
              <span class="mono">{{ post.slug }}</span>
              <span v-if="post.tags.length" class="post-tags">{{
                post.tags.map((t) => `#${t}`).join(" ")
              }}</span>
              <span v-if="post.description" class="post-description">
                {{ post.description }}</span
              >
            </span>
          </span>
          <StatusBadge
            :status="post.status"
            :publish-at="post.publishAt"
            class="post-status"
          />
          <span class="post-words">{{ wordLabel(post.words) }}</span>
          <time
            class="post-edited"
            :datetime="post.publishAt ?? post.publishedOn ?? post.updatedAt"
            :title="`Edited ${exactTime(post.updatedAt)}`"
            >{{ rowDate(post) }}
            <span
              v-if="
                !isPublished(post) &&
                !isScheduled(post) &&
                post.updatedBy &&
                post.updatedBy !== state.username
              "
              class="muted"
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
      <p class="muted">
        Write it here in the browser, or from a terminal with your own editor.
      </p>
      <div class="empty-options">
        <div class="empty-option">
          <UButton icon="i-lucide-plus" size="lg" @click="creating = true"
            >New post</UButton
          >
        </div>
        <div class="empty-option">
          <p class="field-hint">
            In a terminal, after you
            <a
              href="https://github.com/weburz/burzblog/releases/latest"
              target="_blank"
              rel="noopener noreferrer"
              >download burzblog</a
            >:
          </p>
          <pre class="terminal"><code>burzblog login {{ origin }}
burzblog new "My first post"</code></pre>
        </div>
      </div>
    </div>

    <p v-else-if="loading" class="loading" role="status">Loading posts…</p>

    <p v-if="list && list.items.length < list.total" class="list-foot muted">
      Showing {{ list.items.length.toLocaleString() }} of
      {{ list.total.toLocaleString() }}.
      <UButton
        size="xs"
        color="neutral"
        variant="outline"
        class="ml-2"
        :loading="loadingMore"
        @click="load(true)"
        >Show more</UButton
      >
    </p>

    <p
      v-if="shortcutsEnabled && list && list.items.length"
      class="list-foot list-foot-keys muted"
    >
      Press <UKbd value="n" /> for a new post, <UKbd value="/" /> to search,
      <UKbd value="?" /> for all shortcuts.
    </p>

    <NewPostDialog v-model:open="creating" @created="created" />
  </div>
</template>
