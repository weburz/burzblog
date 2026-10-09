<script setup lang="ts">
import { computed, ref, watch } from "vue";
import type { Article } from "../types";
import { ApiError, request, errorMessage } from "../utils/api";
import { withRetainedFocus } from "../utils/focus";
import { slugify } from "../utils/posts";

const open = defineModel<boolean>("open", { default: false });
const emit = defineEmits<{ created: [article: Article] }>();
const title = ref("");
const slug = ref("");
const slugTouched = ref(false);
const error = ref("");
const busy = ref(false);
const newPath = computed(() => `posts/${slug.value.trim() || "…"}.md`);
// Every opening starts from an empty form.
watch(open, (value) => {
  if (!value) return;
  title.value = "";
  slug.value = "";
  slugTouched.value = false;
  error.value = "";
});
watch(title, (value) => {
  if (!slugTouched.value) slug.value = slugify(value);
});
const create = () =>
  withRetainedFocus(async () => {
    error.value = "";
    if (!title.value.trim()) {
      error.value = "Give the post a title.";
      return;
    }
    busy.value = true;
    try {
      const article = await request<Article>("/api/posts", "POST", {
        title: title.value,
        slug: slug.value,
      });
      open.value = false;
      emit("created", article);
    } catch (cause) {
      error.value =
        cause instanceof ApiError && cause.code === "exists"
          ? `${newPath.value} already exists. Choose a different filename.`
          : errorMessage(cause);
    } finally {
      busy.value = false;
    }
  });
</script>

<template>
  <UModal
    v-model:open="open"
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
            title="Lowercase letters, numbers and single hyphens"
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
        <p v-if="error" class="alert" role="alert">{{ error }}</p>
      </form>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" @click="open = false">Cancel</UButton>
        <UButton type="submit" form="new-post-form" color="primary" :loading="busy"
          >Create draft</UButton
        >
      </div>
    </template>
  </UModal>
</template>
