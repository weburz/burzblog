<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import type { Article, Session } from "./types";
import { ApiError, request, errorMessage } from "./utils/api";
import {
  defaultList,
  parseRoute,
  routeSearch,
  sameRoute,
  type ListState,
  type Route,
} from "./utils/route";

const authenticated = ref(false);
const ready = ref(false);
const error = ref("");
const toast = useToast();
// The address is the source of truth for the view, so Back, Forward, reload and
// bookmarks work. The shell is served only at "/", so views are query parameters.
const route = ref<Route>(parseRoute(window.location.search));
const list = ref<ListState>(
  route.value.name === "posts" ? route.value.list : { ...defaultList },
);
// Remount the list only when the address changes it (Back/Forward), not while
// the list itself is updating the address.
const listVersion = ref(0);
const restoreScroll = ref<number | null>(null);
const article = ref<Article | null>(null);
const editorVersion = ref(0);
const shownTitle = ref("");
const opening = ref<string | null>(null);
const reauthenticate = ref(false);
const repository = computed(() => route.value.name === "repository");
const address = (target: Route) => `${window.location.pathname}${routeSearch(target)}`;
// Nuxt's minimal router rewrites every entry's history.state on popstate, so what
// BurzBlog needs to remember about entries lives here instead.
let listScroll: number | null = null;
let cameFromList = false;
const go = (target: Route, mode: "push" | "replace" = "push") => {
  if (mode === "push") window.history.pushState({}, "", address(target));
  else window.history.replaceState({}, "", address(target));
  route.value = target;
};
const basename = (path: string) => path.slice(path.lastIndexOf("/") + 1);
const openArticle = (value: Article) => {
  article.value = value;
  shownTitle.value = value.title;
  editorVersion.value++;
};
const loadPost = async (path: string) => {
  if (article.value?.path === path || opening.value === path) return;
  article.value = null;
  opening.value = path;
  try {
    openArticle(await request<Article>(`/api/post?path=${encodeURIComponent(path)}`));
  } catch (cause) {
    // A bookmark to a moved or deleted post lands on the list with the reason.
    if (cause instanceof ApiError && cause.code === "authentication") return;
    toast.add({
      title: `Could not open ${path}`,
      description:
        cause instanceof ApiError && cause.code === "not_found"
          ? "It is no longer on disk; it may have been moved or deleted."
          : errorMessage(cause),
      color: "error",
    });
    go({ name: "posts", list: list.value }, "replace");
  } finally {
    if (opening.value === path) opening.value = null;
  }
};
// Apply an address the browser already shows (Back or Forward).
const apply = (target: Route) => {
  const from = route.value;
  route.value = target;
  if (target.name === "post") {
    cameFromList = from.name === "posts";
    if (authenticated.value) void loadPost(target.path);
    return;
  }
  article.value = null;
  if (target.name === "posts") {
    list.value = target.list;
    // Returning from a post puts the list back where the writer left it.
    restoreScroll.value = from.name === "post" ? listScroll : null;
    listVersion.value++;
  }
};
const editor = ref<{
  dirty: boolean;
  requestLeave: (onLeave: () => void, onStay?: () => void) => void;
} | null>(null);
// Set when BurzBlog itself steps back, after any unsaved-writing question.
let steppingBack = false;
const stepBack = () => {
  steppingBack = true;
  window.history.back();
};
const popped = () => {
  const target = parseRoute(window.location.search);
  if (steppingBack) {
    steppingBack = false;
    apply(target);
    return;
  }
  const current = route.value;
  const leaving =
    current.name === "post" &&
    editor.value?.dirty &&
    !(target.name === "post" && target.path === current.path);
  if (!leaving) {
    apply(target);
    return;
  }
  // The browser has already moved; put the editor's address back if the writer
  // chooses to keep editing.
  editor.value!.requestLeave(
    () => apply(target),
    () => window.history.pushState({}, "", address(current)),
  );
};
const navigate = (target: "posts" | "repository") => {
  const next: Route =
    target === "repository"
      ? { name: "repository" }
      : { name: "posts", list: list.value };
  if (sameRoute(next, route.value)) return;
  const proceed = () => {
    article.value = null;
    if (next.name === "posts") {
      restoreScroll.value = null;
      listVersion.value++;
    }
    go(next);
  };
  if (route.value.name === "post" && editor.value) editor.value.requestLeave(proceed);
  else proceed();
};
const opened = (value: Article) => {
  // Remember where the list was, so Back returns to the same place.
  listScroll = window.scrollY;
  cameFromList = true;
  openArticle(value);
  go({ name: "post", path: value.path });
  window.scrollTo(0, 0);
};
const closeEditor = () => {
  if (cameFromList) stepBack();
  else {
    article.value = null;
    listVersion.value++;
    go({ name: "posts", list: list.value });
  }
};
const archived = (path: string) => {
  toast.add({
    title: "Post archived",
    description: `${path} moved to .archive/ and is still on disk.`,
    color: "success",
  });
  closeEditor();
};
const changed = (value: Article) => {
  shownTitle.value = value.title;
  if (route.value.name === "post" && route.value.path !== value.path)
    go({ name: "post", path: value.path }, "replace");
};
const listChanged = (value: ListState) => {
  // A list request that finishes after the writer moved on must not pull them back.
  if (route.value.name !== "posts") return;
  const previous = route.value.list;
  list.value = value;
  // A new page is a step worth returning to; filter and search refine in place.
  go(
    { name: "posts", list: value },
    previous.offset !== value.offset ? "push" : "replace",
  );
};
onMounted(async () => {
  window.addEventListener("popstate", popped);
  try {
    authenticated.value = (await request<Session>("/api/session")).authenticated;
  } catch (cause) {
    error.value = errorMessage(cause);
  } finally {
    ready.value = true;
  }
  if (authenticated.value && route.value.name === "post") void loadPost(route.value.path);
});
onUnmounted(() => window.removeEventListener("popstate", popped));
const title = computed(() => {
  if (!ready.value) return "BurzBlog";
  if (!authenticated.value) return "Sign in · BurzBlog";
  if (route.value.name === "repository") return "Repository · BurzBlog";
  if (route.value.name === "post")
    return `${shownTitle.value || basename(route.value.path)} · BurzBlog`;
  return "Posts · BurzBlog";
});
watch(title, (value) => (document.title = value), { immediate: true });
// Where the writer was when the session ran out, so signing in again returns there.
// Busy controls drop focus to <body> while a request runs, so remember the last
// focused control rather than reading document.activeElement at expiry time.
let lastFocus: HTMLElement | null = null;
let resumeFocus: HTMLElement | null = null;
const remember = (event: FocusEvent) => {
  const target = event.target;
  if (target instanceof HTMLElement && !target.closest("[role=dialog]"))
    lastFocus = target;
};
const signedIn = () => {
  const resuming = reauthenticate.value;
  authenticated.value = true;
  reauthenticate.value = false;
  error.value = "";
  if (!resuming) {
    // A bookmarked post opens once the writer has signed in.
    if (route.value.name === "post") void loadPost(route.value.path);
    return;
  }
  window.dispatchEvent(new CustomEvent("burzblog:session-restored"));
  if (route.value.name === "post" && !article.value) void loadPost(route.value.path);
  const target = resumeFocus;
  resumeFocus = null;
  // The dialog returns focus as it closes; land on the interrupted control after it.
  void nextTick(() =>
    setTimeout(() => {
      if (target?.isConnected && !(target as HTMLButtonElement).disabled)
        target.focus({ preventScroll: true });
    }, 250),
  );
};
const confirmingLogout = ref(false);
const showReauthenticate = () => {
  if (!authenticated.value || reauthenticate.value) return;
  resumeFocus = lastFocus;
  reauthenticate.value = true;
};
onMounted(() => {
  window.addEventListener("burzblog:session-expired", showReauthenticate);
  document.addEventListener("focusin", remember);
});
onUnmounted(() => {
  window.removeEventListener("burzblog:session-expired", showReauthenticate);
  document.removeEventListener("focusin", remember);
});
const signOut = async () => {
  confirmingLogout.value = false;
  try {
    await request("/api/logout", "POST", {});
  } catch (cause) {
    // An already-expired session is as good as signed out.
    if (!(cause instanceof ApiError && cause.code === "authentication")) {
      error.value = errorMessage(cause);
      return;
    }
  }
  authenticated.value = false;
  article.value = null;
  reauthenticate.value = false;
  error.value = "";
  // The address can name a post; do not leave it behind for the next sign-in.
  list.value = { ...defaultList };
  listVersion.value++;
  go({ name: "posts", list: list.value }, "replace");
};
const logout = () => {
  if (article.value && editor.value?.dirty) {
    confirmingLogout.value = true;
    return;
  }
  return signOut();
};
</script>

<template>
  <UApp>
    <div class="shell">
      <a href="#main" class="sr-only focus:not-sr-only">Skip to content</a>
      <header class="topbar">
        <div class="topbar-inner">
          <span class="brand"
            ><span class="brand-mark" aria-hidden="true" />BurzBlog</span
          >
          <nav v-if="authenticated" aria-label="Main" class="nav-tabs">
            <button
              class="nav-tab"
              :aria-current="!repository ? 'page' : undefined"
              @click="navigate('posts')"
            >
              Posts
            </button>
            <button
              class="nav-tab"
              :aria-current="repository ? 'page' : undefined"
              @click="navigate('repository')"
            >
              Repository
            </button>
          </nav>
          <UButton
            v-if="authenticated"
            color="neutral"
            variant="ghost"
            icon="i-lucide-log-out"
            aria-label="Sign out"
            @click="logout"
            ><span class="max-[600px]:hidden">Sign out</span></UButton
          >
        </div>
      </header>
      <main id="main" class="content">
        <p v-if="error" class="alert" role="alert">{{ error }}</p>
        <p v-if="!ready" class="muted py-12 text-center" role="status">
          Opening your writing space…
        </p>
        <LoginForm v-else-if="!authenticated" class="mt-16" @success="signedIn" />
        <template v-else>
          <ArticleEditor
            v-if="route.name === 'post' && article"
            :key="editorVersion"
            ref="editor"
            :article="article"
            @close="closeEditor"
            @archived="archived"
            @changed="changed"
          />
          <p
            v-else-if="route.name === 'post'"
            class="muted py-12 text-center"
            role="status"
          >
            Opening {{ route.path }}…
          </p>
          <RepositoryPanel v-else-if="repository" />
          <PostsView
            v-else
            :key="listVersion"
            :initial="list"
            :restore-scroll="restoreScroll"
            @open="opened"
            @change="listChanged"
          />
        </template>
      </main>
      <UModal
        v-model:open="reauthenticate"
        title="Your session ended"
        :description="
          article
            ? 'Sign in to continue. Your unsaved writing stays in the editor.'
            : 'Sign in to continue where you left off.'
        "
      >
        <template #body>
          <LoginForm compact @success="signedIn">
            <template #actions>
              <UButton
                type="button"
                color="neutral"
                variant="ghost"
                size="lg"
                @click="reauthenticate = false"
                >Not now</UButton
              >
            </template>
          </LoginForm>
        </template>
      </UModal>
      <ConfirmDialog
        v-model:open="confirmingLogout"
        title="Sign out and close the editor?"
        body="Unsaved writing will be lost. Copy anything you want to keep first."
        confirm-label="Sign out"
        confirm-color="error"
        @confirm="signOut"
      />
    </div>
  </UApp>
</template>
