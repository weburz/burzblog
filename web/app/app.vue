<script setup lang="ts">
import { nextTick, onMounted, onUnmounted, ref } from "vue";
import type { Article, Session } from "./types";
import { ApiError, request, errorMessage } from "./utils/api";

const authenticated = ref(false);
const ready = ref(false);
const error = ref("");
const article = ref<Article | null>(null);
const repository = ref(false);
const reauthenticate = ref(false);
onMounted(async () => {
  try {
    authenticated.value = (await request<Session>("/api/session")).authenticated;
  } catch (cause) {
    error.value = errorMessage(cause);
  } finally {
    ready.value = true;
  }
});
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
  if (!resuming) return;
  window.dispatchEvent(new CustomEvent("burzblog:session-restored"));
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
const editor = ref<{
  dirty: boolean;
  requestLeave: (onLeave: () => void) => void;
} | null>(null);
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
const show = (target: "posts" | "repository") => {
  article.value = null;
  repository.value = target === "repository";
};
const navigate = (target: "posts" | "repository") => {
  if (article.value && editor.value) {
    editor.value.requestLeave(() => show(target));
    return;
  }
  show(target);
};
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
  repository.value = false;
  reauthenticate.value = false;
  error.value = "";
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
            v-if="article"
            :key="article.path"
            ref="editor"
            :article="article"
            @close="article = null"
          />
          <RepositoryPanel v-else-if="repository" />
          <PostsView v-else @open="article = $event" />
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
