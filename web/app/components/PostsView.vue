<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import type { Article, PostList } from "../types";
import { request, errorMessage } from "../utils/api";

const emit = defineEmits<{ open: [article: Article] }>();
const posts = ref<PostList>({ items: [], total: 0 });
const offset = ref(0);
const search = ref("");
const drafts = ref(false);
const creating = ref(false);
const title = ref("");
const slug = ref("");
const error = ref("");
const busy = ref(false);
const filtered = computed(() =>
  posts.value.items.filter(
    (post) =>
      (!drafts.value || post.status === "draft") &&
      post.title.toLowerCase().includes(search.value.toLowerCase()),
  ),
);
const refresh = async (next = offset.value) => {
  busy.value = true;
  error.value = "";
  try {
    posts.value = await request<PostList>(`/api/posts?offset=${next}&limit=100`);
    offset.value = next;
  } catch (cause) {
    error.value = errorMessage(cause);
  } finally {
    busy.value = false;
  }
};
const open = async (path: string) => {
  try {
    emit("open", await request<Article>(`/api/post?path=${encodeURIComponent(path)}`));
  } catch (cause) {
    error.value = errorMessage(cause);
  }
};
const create = async () => {
  busy.value = true;
  error.value = "";
  try {
    emit(
      "open",
      await request<Article>("/api/posts", "POST", {
        title: title.value,
        slug: slug.value,
      }),
    );
  } catch (cause) {
    error.value = errorMessage(cause);
  } finally {
    busy.value = false;
  }
};
onMounted(() => refresh());
</script>

<template>
  <section class="panel">
    <div class="toolbar mb-8">
      <div>
        <p class="muted text-sm mb-2">YOUR WRITING</p>
        <h1 class="page-title">Posts</h1>
      </div>
      <UButton color="secondary" size="lg" @click="creating = !creating">{{
        creating ? "Cancel new post" : "New post"
      }}</UButton>
    </div>
    <form v-if="creating" class="my-8 max-w-xl" @submit.prevent="create">
      <div class="field">
        <label for="new-title">Title</label
        ><UInput
          id="new-title"
          v-model="title"
          required
          maxlength="500"
          class="w-full"
          size="xl"
        />
      </div>
      <div class="field">
        <label for="new-slug">Filename / slug</label
        ><UInput
          id="new-slug"
          v-model="slug"
          required
          pattern="[a-z0-9]+(?:-[a-z0-9]+)*"
          class="w-full"
          placeholder="my-first-post"
          aria-describedby="slug-hint"
        />
        <p id="slug-hint" class="muted text-sm">
          Lowercase letters, numbers and hyphens. Creates an ordinary file in posts/.
        </p>
      </div>
      <UButton type="submit" color="secondary" :loading="busy">Create draft</UButton>
    </form>
    <div class="toolbar">
      <div class="actions">
        <UButton
          color="neutral"
          :variant="drafts ? 'ghost' : 'soft'"
          @click="drafts = false"
          >All posts</UButton
        ><UButton
          color="neutral"
          :variant="drafts ? 'soft' : 'ghost'"
          @click="drafts = true"
          >Drafts</UButton
        >
      </div>
      <div class="actions">
        <UInput
          v-model="search"
          aria-label="Filter this page of posts"
          placeholder="Filter this page…"
        /><UButton color="neutral" variant="ghost" :loading="busy" @click="refresh()"
          >Refresh</UButton
        >
      </div>
    </div>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <p v-if="!busy && !filtered.length" class="muted py-16">
      No posts here yet. Start with a new draft, or add a Markdown file to your content
      directory.
    </p>
    <ul aria-label="Posts">
      <li v-for="post in filtered" :key="post.path">
        <button class="post-row" @click="open(post.path)">
          <span class="post-title">{{ post.title }}</span
          ><span class="block muted mt-2 text-sm"
            >{{
              post.status === "published"
                ? "Published"
                : post.status === "unavailable"
                  ? "Needs attention"
                  : "Draft"
            }}
            · {{ post.path }}</span
          >
        </button>
      </li>
    </ul>
    <div class="toolbar mt-6">
      <p class="muted text-sm">{{ posts.total }} files · reads directly from disk</p>
      <div class="actions">
        <UButton
          v-if="offset > 0"
          color="neutral"
          variant="ghost"
          @click="refresh(offset - 100)"
          >Previous</UButton
        ><UButton
          v-if="offset + 100 < posts.total"
          color="neutral"
          variant="ghost"
          @click="refresh(offset + 100)"
          >Next</UButton
        >
      </div>
    </div>
  </section>
</template>
