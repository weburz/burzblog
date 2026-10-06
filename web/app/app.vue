<script setup lang="ts">
import { onMounted, onUnmounted, ref } from "vue";
import type { Article, Session } from "./types";
import { request, errorMessage } from "./utils/api";

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
const signedIn = () => {
  authenticated.value = true;
  reauthenticate.value = false;
  error.value = "";
};
const editor = ref<{ dirty: boolean } | null>(null);
const confirmingLeave = ref(false);
const confirmingLogout = ref(false);
const pendingRepository = ref(false);
const showReauthenticate = () => {
  reauthenticate.value = true;
};
onMounted(() => window.addEventListener("burzblog:session-expired", showReauthenticate));
onUnmounted(() =>
  window.removeEventListener("burzblog:session-expired", showReauthenticate),
);
const show = (target: "posts" | "repository") => {
  article.value = null;
  repository.value = target === "repository";
};
const navigate = (target: "posts" | "repository") => {
  if (article.value && editor.value?.dirty) {
    pendingRepository.value = target === "repository";
    confirmingLeave.value = true;
    return;
  }
  show(target);
};
const leave = () => {
  confirmingLeave.value = false;
  show(pendingRepository.value ? "repository" : "posts");
};
const signOut = async () => {
  confirmingLogout.value = false;
  try {
    await request("/api/logout", "POST", {});
    authenticated.value = false;
    article.value = null;
    repository.value = false;
    reauthenticate.value = false;
  } catch (cause) {
    error.value = errorMessage(cause);
  }
};
const logout = () => {
  if (article.value && editor.value?.dirty) {
    confirmingLogout.value = true;
    return;
  }
  return signOut();
};
const accountItems = [
  [
    { label: "Sign in again", icon: "i-lucide-key-round", onSelect: showReauthenticate },
    { label: "Sign out", icon: "i-lucide-log-out", onSelect: logout },
  ],
];
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
          <UDropdownMenu v-if="authenticated" :items="accountItems">
            <UButton
              color="neutral"
              variant="ghost"
              icon="i-lucide-user-round"
              trailing-icon="i-lucide-chevron-down"
              aria-label="Account"
              ><span class="max-[600px]:hidden">Account</span></UButton
            >
          </UDropdownMenu>
        </div>
      </header>
      <main id="main" class="content">
        <p v-if="error" class="alert" role="alert">{{ error }}</p>
        <p v-if="!ready" class="muted py-12 text-center" role="status">
          Opening your writing space…
        </p>
        <LoginForm v-else-if="!authenticated" class="mt-16" @success="signedIn" />
        <template v-else>
          <section
            v-if="reauthenticate"
            class="reauth"
            role="region"
            aria-label="Sign in again"
          >
            <h2 class="section-title">Your session ended</h2>
            <p class="muted mt-1">
              Sign in to continue. Unsaved writing stays in the editor below.
            </p>
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
          </section>
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
      <ConfirmDialog
        v-model:open="confirmingLeave"
        title="Leave without saving?"
        body="Unsaved writing will be lost. Copy anything you want to keep first."
        confirm-label="Leave"
        confirm-color="error"
        @confirm="leave"
      />
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
