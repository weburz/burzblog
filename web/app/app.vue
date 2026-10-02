<script setup lang="ts">
import { onMounted, ref } from "vue";
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
const logout = async () => {
  if (
    article.value &&
    !window.confirm(
      "Sign out and close the editor? Save or copy any unsaved writing first.",
    )
  ) {
    return;
  }
  try {
    await request("/api/logout", "POST", {});
    authenticated.value = false;
    article.value = null;
    repository.value = false;
  } catch (cause) {
    error.value = errorMessage(cause);
  }
};
</script>

<template>
  <UApp>
    <div class="shell">
      <a href="#main" class="sr-only focus:not-sr-only">Skip to content</a>
      <header class="topbar">
        <span class="brand"><span class="brand-mark" aria-hidden="true" />BurzBlog</span>
        <nav v-if="authenticated" aria-label="Main navigation" class="actions">
          <UButton
            v-if="!article"
            color="neutral"
            variant="ghost"
            @click="repository = !repository"
            >{{ repository ? "Posts" : "Repository" }}</UButton
          >
          <UButton
            color="neutral"
            variant="ghost"
            @click="reauthenticate = !reauthenticate"
            >Sign in again</UButton
          >
          <UButton color="neutral" variant="ghost" @click="logout">Sign out</UButton>
        </nav>
      </header>
      <main id="main">
        <p v-if="error" class="error" role="alert">{{ error }}</p>
        <p v-if="!ready" class="muted py-12" role="status">Opening your writing space…</p>
        <LoginForm v-else-if="!authenticated" class="mx-auto mt-16" @success="signedIn" />
        <template v-else>
          <LoginForm v-if="reauthenticate" @success="signedIn" />
          <ArticleEditor
            v-if="article"
            :key="article.path"
            :article="article"
            @close="article = null"
          />
          <RepositoryPanel v-else-if="repository" />
          <PostsView v-else @open="article = $event" />
        </template>
      </main>
    </div>
  </UApp>
</template>
