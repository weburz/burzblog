<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import type { PostList, PostSummary } from "../types";
import { request } from "../utils/api";
import {
  actionCommands,
  goToCommands,
  opensPalette,
  paletteGroups,
} from "../utils/palette";
import { editedLabel } from "../utils/time";

const open = defineModel<boolean>("open", { default: false });
const emit = defineEmits<{ newPost: []; signOut: [] }>();

const { commands: contextual } = useRegisteredCommands();
const { recent } = useRecentPosts();
const theme = usePreferredTheme();
const shortcutsOpen = useState("shortcuts-open", () => false);

const query = ref("");
const posts = ref<PostSummary[]>([]);
const loading = ref(false);

// A modifier shortcut, so it ignores the single-key shortcuts switch and works
// while typing. In the Markdown body plain Ctrl/Cmd+K stays the link shortcut.
const toggleKey = (event: KeyboardEvent) => {
  const target = event.target as HTMLElement | null;
  if (!opensPalette(event, Boolean(target?.closest(".md-source")))) return;
  event.preventDefault();
  open.value = !open.value;
};
onMounted(() => window.addEventListener("keydown", toggleKey));
onUnmounted(() => window.removeEventListener("keydown", toggleKey));

// Matches come from the server; a slower, older answer must not win.
let sequence = 0;
const load = async () => {
  const ticket = ++sequence;
  loading.value = true;
  const search = query.value.trim();
  try {
    const result = await request<PostList>(
      `/api/posts${search ? `?${new URLSearchParams({ q: search })}` : ""}`,
    );
    if (ticket === sequence) posts.value = result.items;
  } catch {
    if (ticket === sequence) posts.value = [];
  } finally {
    if (ticket === sequence) loading.value = false;
  }
};
let debounce: ReturnType<typeof setTimeout> | undefined;
watch(query, () => {
  clearTimeout(debounce);
  debounce = setTimeout(load, 200);
});
watch(open, (value) => {
  clearTimeout(debounce);
  if (!value) return;
  query.value = "";
  void load();
});
onUnmounted(() => clearTimeout(debounce));

// The palette closes first, so a dialog a command opens keeps the focus.
const run = async (fn: () => void) => {
  open.value = false;
  await nextTick();
  fn();
};

const goTo = goToCommands((path) => void navigateTo(path));
const actions = actionCommands({
  newPost: () => emit("newPost"),
  theme: (value) => (theme.preference.value = value),
  shortcuts: () => (shortcutsOpen.value = true),
  signOut: () => emit("signOut"),
});

const groups = computed(() =>
  paletteGroups({
    query: query.value,
    posts: posts.value,
    recent: recent.value,
    contextual: contextual.value,
    goTo,
    actions,
    run: (fn) => void run(fn),
    open: (slug) => void run(() => void navigateTo(`/posts/${encodeURIComponent(slug)}`)),
  }),
);
const announcement = computed(() => {
  if (!query.value.trim() || loading.value) return "";
  const count = posts.value.length;
  return `${count} ${count === 1 ? "post matches" : "posts match"}`;
});
</script>

<template>
  <UModal
    v-model:open="open"
    title="Search or jump"
    description="Find a post, go to a page, or run an action."
    :ui="{ content: 'max-w-xl' }"
  >
    <template #content>
      <UCommandPalette
        v-model:search-term="query"
        :groups="groups"
        :loading="loading"
        preserve-group-order
        placeholder="Search posts, jump to a page, or run an action…"
        close
        class="palette"
        @update:open="open = $event"
      >
        <template #post-trailing="{ item }">
          <span class="palette-meta">
            <StatusBadge :status="item.status" />
            <span class="palette-edited">{{ editedLabel(item.updatedAt) }}</span>
          </span>
        </template>
      </UCommandPalette>
      <p class="sr-only" role="status" aria-live="polite">{{ announcement }}</p>
    </template>
  </UModal>
</template>
