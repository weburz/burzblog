<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import type { Post, PostList, PostStatus, PostSummary, TagCount } from "../types";
import { errorMessage, request } from "../utils/api";
import { wordLabel } from "../utils/markdown";
import { duplicatePost } from "../utils/duplicate";
import { returnFocusTo } from "../utils/focus";
import {
  actionSummary,
  addTag,
  applyAction,
  hasUndo,
  outcomeSummary,
  partitionByAction,
  reverseAction,
  runSequentially,
  type PostAction,
} from "../utils/post-actions";
import { noSiteUrlNotice, publicPostUrl } from "../utils/site";
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
const toast = useToast();
const { state, isAdmin } = useCurrentSession();
const { enabled: shortcutsEnabled } = useEnabledShortcuts();
const { now } = useCurrentTime();

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
const { creating, create: createPost } = useCreatedPost();

// The server sends one page at a time, so a big blog opens fast; "Show more"
// asks for the next one.
const pageSize = 100;
const maxPageSize = 500;
let sequence = 0;
// A quiet load refreshes the rows already shown, without the busy dimming,
// so an action leaves the filters and the scroll position alone.
const load = async (more = false, quiet = false) => {
  const ticket = ++sequence;
  loading.value = !more && !quiet;
  loadingMore.value = more;
  error.value = "";
  const limit = quiet
    ? Math.min(maxPageSize, Math.max(pageSize, list.value?.items.length ?? 0))
    : pageSize;
  const query = new URLSearchParams({ limit: String(limit) });
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
const heading = ref<HTMLElement | null>(null);
// The slug of the post the writer came back from, when Back from the editor
// brought them here; null for any other way of arriving.
const leftPost = (): string | null => {
  const back = (router.options.history.state as { back?: string | null }).back;
  const slug = /^\/posts\/([^/?#]+)/.exec(back ?? "")?.[1];
  return slug ? decodeURIComponent(slug) : null;
};
// Whether focus has nowhere useful to be: on the page itself or nothing.
const focusLost = () => {
  const active = document.activeElement;
  return !active || active === document.body || active.id === "main";
};
const rowLink = (slug: string) =>
  document.querySelector<HTMLElement>(`[data-slug="${CSS.escape(slug)}"] .post-row`);
// Focus lands on the post's row when it is listed, else on the heading, unless
// the writer has already moved it somewhere.
const focusRowOrHeading = async (slug: string) => {
  await nextTick();
  if (focusLost()) (rowLink(slug) ?? heading.value)?.focus();
};
onMounted(() => {
  const from = leftPost();
  void load().then(() => (from ? focusRowOrHeading(from) : undefined));
  void loadTags();
});
const refresh = () => Promise.all([load(false, true), loadTags()]);

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
      : editedLabel(post.updatedAt, now.value);

// Rows can be ticked for the bulk bar; the selection only ever holds rows
// that are still listed.
const selected = ref<string[]>([]);
const items = computed(() => list.value?.items ?? []);
const selectedPosts = computed(() =>
  items.value.filter((p) => selected.value.includes(p.slug)),
);
const allSelected = computed(
  () => items.value.length > 0 && selectedPosts.value.length === items.value.length,
);
watch(items, (rows) => {
  selected.value = selected.value.filter((slug) => rows.some((p) => p.slug === slug));
});
const toggleSelected = (slug: string, on = !selected.value.includes(slug)) => {
  selected.value = on
    ? [...selected.value.filter((s) => s !== slug), slug]
    : selected.value.filter((s) => s !== slug);
};
const toggleAll = (on: boolean) =>
  (selected.value = on ? items.value.map((p) => p.slug) : []);
const clearSelection = () => (selected.value = []);
watch([status, search, tag], clearSelection);

// One post or a whole selection: run in turn, report once, refresh in place.
const working = ref(false);
// A row or bulk action can remove the control that had focus; hand it to the
// next sensible one unless something else already took it.
const focusAfterAction = async (target: () => HTMLElement | null | undefined) => {
  await nextTick();
  if (focusLost()) target()?.focus();
};
const selectAllBox = () => document.getElementById("select-all-posts");
const perform = async (action: PostAction, targets: PostSummary[], bulk = false) => {
  const { apply, skipped } = partitionByAction(action, targets, isAdmin.value);
  if (!apply.length) {
    toast.add({
      title: "Nothing to do",
      description: "None of those posts can be changed that way.",
      color: "neutral",
    });
    return;
  }
  working.value = true;
  // After deleting one row, focus goes to the row below it, if there is one.
  const below = items.value[items.value.findIndex((p) => p.slug === apply[0]?.slug) + 1];
  try {
    const { done, failed } = await runSequentially(apply, (p) =>
      applyAction(p.slug, action, p.version),
    );
    const names = done.map((d) => d.item.title || d.item.slug);
    const summary = actionSummary(action, names, failed, skipped.length);
    const undo =
      hasUndo(action) && done.length
        ? () => undoAll(action, done as { item: PostSummary; result: Post }[])
        : null;
    toast.add({
      ...summary,
      icon: "i-lucide-check",
      ...(undo && {
        duration: 10000,
        actions: [{ label: "Undo", color: "neutral", variant: "outline", onClick: undo }],
      }),
    });
    clearSelection();
    await refresh();
    if (bulk) await focusAfterAction(selectAllBox);
    else if (action === "delete")
      await focusAfterAction(() => (below && rowLink(below.slug)) || heading.value);
  } finally {
    working.value = false;
  }
};
// The reverse of each finished change, from the post that change returned.
const undoAll = async (
  action: PostAction,
  done: { item: PostSummary; result: Post }[],
) => {
  const { done: undone, failed } = await runSequentially(
    done.map((d) => d.item),
    (item) =>
      reverseAction(
        action,
        item.status === "published",
        done.find((d) => d.item.slug === item.slug)!.result,
      ),
  );
  toast.add(
    outcomeSummary(
      "Restored",
      undone.map((d) => d.item.title || d.item.slug),
      failed,
    ),
  );
  await refresh();
};

const confirm = reactive({
  open: false,
  action: "delete" as PostAction,
  bulk: false,
  targets: [] as PostSummary[],
});
const confirmOpener = ref<HTMLElement | null>(null);
const closeFocus = returnFocusTo(() => confirmOpener.value);
// Deleting always asks; so does publishing a selection, which goes out to the
// feeds, the sitemap and the deploy hook. One row's Publish has an Undo instead.
const ask = (action: PostAction, targets: PostSummary[], bulk = false) => {
  const { apply } = partitionByAction(action, targets, isAdmin.value);
  if (!apply.length || !(action === "delete" || (action === "publish" && bulk)))
    return void perform(action, targets, bulk);
  const first = apply[0];
  confirmOpener.value = bulk
    ? (document.activeElement as HTMLElement | null)
    : first
      ? document.querySelector<HTMLElement>(
          `[data-slug="${CSS.escape(first.slug)}"] .post-menu`,
        )
      : null;
  Object.assign(confirm, { open: true, action, bulk, targets: apply });
};
const confirmText = computed(() => {
  const count = confirm.targets.length;
  const many = count !== 1;
  return confirm.action === "publish"
    ? {
        title: many ? `Publish ${count} posts?` : "Publish this post?",
        body: `${many ? "They become" : "It becomes"} part of your site on the next build. You can unpublish ${many ? "them" : "it"} later.`,
        label: many ? "Publish posts" : "Publish post",
        color: "primary" as const,
      }
    : {
        title: many ? `Delete ${count} posts for good?` : "Delete this post for good?",
        body: "The posts and their whole history are removed. This cannot be undone.",
        label: many ? "Delete posts" : "Delete post",
        color: "error" as const,
      };
});
const confirmRun = () => {
  // The selection is cleared by the action, so the bar's button is not where
  // the dialog should hand focus back to.
  if (confirm.bulk) confirmOpener.value = selectAllBox();
  confirm.open = false;
  void perform(confirm.action, confirm.targets, confirm.bulk);
};

const duplicate = async (post: PostSummary) => {
  try {
    const copy = await duplicatePost(post.slug);
    toast.add({
      title: "Draft copy created",
      description: `Now editing ${copy.slug}.`,
      color: "success",
    });
    await navigateTo(`/posts/${copy.slug}`);
  } catch (cause) {
    toast.add({
      title: "Could not duplicate",
      description: errorMessage(cause),
      color: "error",
    });
  }
};
const copyLink = async (post: PostSummary) => {
  const url = publicPostUrl(state.value.postUrl, post.slug);
  if (!url) return void toast.add(noSiteUrlNotice);
  try {
    await navigator.clipboard.writeText(url);
    toast.add({ title: "Link copied", description: url, color: "success" });
  } catch {
    toast.add({ title: "Could not copy", description: url, color: "neutral" });
  }
};

const tagSelected = async (raw: string) => {
  working.value = true;
  try {
    const { done, failed } = await runSequentially(selectedPosts.value, (p) =>
      addTag(p.slug, raw),
    );
    const changed = done.filter((d) => d.result);
    toast.add({
      ...outcomeSummary(
        "Tagged",
        changed.map((d) => d.item.title || d.item.slug),
        failed,
        done.length - changed.length,
        "they already have that tag",
      ),
      icon: "i-lucide-check",
    });
    clearSelection();
    await refresh();
    await focusAfterAction(selectAllBox);
  } finally {
    working.value = false;
  }
};

// One Tab stop per row: the tick and the menu are only reachable from the row
// that last had focus (j and k move between rows).
const stopSlug = ref("");
const trackRow = (event: FocusEvent) => {
  const slug = rowOf(event.target as HTMLElement)?.dataset.slug;
  if (slug) stopSlug.value = slug;
};

// Rows move with j and k by focusing their link, so Enter opens the post the
// way it does for any link; x ticks the row that has focus.
const rowOf = (target: HTMLElement | null) =>
  target?.closest<HTMLElement>(".post-item") ?? null;
const moveFocus = (target: HTMLElement | null, step: 1 | -1) => {
  const rows = Array.from(document.querySelectorAll<HTMLElement>(".post-item"));
  const current = rowOf(target);
  const at = current ? rows.indexOf(current) : -1;
  const next = at < 0 ? rows[0] : rows[at + step];
  next?.querySelector<HTMLElement>(".post-row")?.focus();
};

// "/" jumps to search and "n" starts a post, unless the writer is typing.
const shortcut = (event: KeyboardEvent) => {
  const target = event.target as HTMLElement | null;
  if (!shortcutsEnabled.value || event.metaKey || event.ctrlKey || event.altKey) return;
  if (
    target?.closest(
      "input, textarea, [contenteditable], [role=dialog], [role=combobox], [role=listbox], [role=menu]",
    )
  )
    return;
  if (event.key === "/") {
    event.preventDefault();
    document.querySelector<HTMLInputElement>("#post-search")?.focus();
  } else if (event.key === "n") {
    event.preventDefault();
    void createPost();
  } else if (event.key === "j" || event.key === "k") {
    event.preventDefault();
    moveFocus(target, event.key === "j" ? 1 : -1);
  } else if (event.key === "x") {
    const slug = rowOf(target)?.dataset.slug;
    if (!slug) return;
    event.preventDefault();
    toggleSelected(slug);
  } else if (event.key === "Escape" && selected.value.length) {
    clearSelection();
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
        <h1 ref="heading" class="page-title" tabindex="-1">Posts</h1>
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
      <UButton
        v-if="!fresh"
        icon="i-lucide-plus"
        size="lg"
        class="shrink-0 whitespace-nowrap"
        :loading="creating"
        @click="createPost"
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

    <div v-if="list && list.items.length" class="post-select-bar">
      <UCheckbox
        id="select-all-posts"
        :model-value="allSelected"
        :indeterminate="selected.length > 0 && !allSelected"
        aria-label="Select all posts shown"
        class="post-select"
        :class="{ 'post-select-on': selected.length }"
        @update:model-value="(v: boolean | 'indeterminate') => toggleAll(v === true)"
      />
      <PostBulkBar
        v-if="selected.length"
        :selected="selectedPosts"
        :admin="isAdmin"
        :busy="working"
        @act="(action) => ask(action, selectedPosts, true)"
        @tag="tagSelected"
        @clear="clearSelection"
      />
      <label v-else for="select-all-posts" class="muted">Select all</label>
    </div>

    <ul
      v-if="list && list.items.length"
      class="post-list"
      :aria-busy="loading"
      @focusin="trackRow"
    >
      <li
        v-for="post in list.items"
        :key="post.slug"
        class="post-item"
        :class="{ 'post-item-selected': selected.includes(post.slug) }"
        :data-slug="post.slug"
      >
        <UCheckbox
          :model-value="selected.includes(post.slug)"
          :aria-label="`Select ${post.title || post.slug}`"
          :tabindex="stopSlug === post.slug ? undefined : -1"
          class="post-select"
          :class="{ 'post-select-on': selected.length }"
          @update:model-value="
            (v: boolean | 'indeterminate') => toggleSelected(post.slug, v === true)
          "
        />
        <NuxtLink :to="`/posts/${post.slug}`" class="post-row">
          <span class="post-main">
            <span
              class="post-title"
              :class="{ 'post-untitled': !post.title }"
              :title="post.title || undefined"
              >{{ post.title || "Untitled" }}</span
            >
            <span class="post-sub">
              <span v-if="post.featured" class="post-featured"
                ><UIcon
                  name="i-lucide-star"
                  class="size-3.5"
                  aria-hidden="true"
                />Featured</span
              >
              <span class="mono" :title="post.slug">{{ post.slug }}</span>
              <span
                v-if="post.tags.length"
                class="post-tags"
                :title="post.tags.map((t) => `#${t}`).join(' ')"
                >{{ post.tags.map((t) => `#${t}`).join(" ") }}</span
              >
              <span
                v-if="post.description"
                class="post-description"
                :title="post.description"
              >
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
        <PostRowMenu
          :post="post"
          :admin="isAdmin"
          :tabbable="stopSlug === post.slug"
          @act="(action) => ask(action, [post])"
          @duplicate="duplicate(post)"
          @copy-link="copyLink(post)"
        />
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
          <UButton icon="i-lucide-plus" size="lg" :loading="creating" @click="createPost"
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

    <ConfirmDialog
      v-model:open="confirm.open"
      :title="confirmText.title"
      :body="confirmText.body"
      :confirm-label="confirmText.label"
      :confirm-color="confirmText.color"
      :close-focus="closeFocus"
      @confirm="confirmRun"
    />
  </div>
</template>
