<script setup lang="ts">
import { computed, ref, watch } from "vue";
import type { Post } from "../types";
import { ApiError, errorMessage, request } from "../utils/api";
import { slugify } from "../utils/slug";

const open = defineModel<boolean>("open", { default: false });
const emit = defineEmits<{ created: [post: Post] }>();
const title = ref("");
const slug = ref("");
// Until the writer edits the slug, it follows the title.
const slugTouched = ref(false);
const busy = ref(false);
const error = ref("");
const slugError = ref("");

watch(title, (value) => {
  if (!slugTouched.value) slug.value = slugify(value);
});
watch(open, (value) => {
  if (!value) return;
  title.value = "";
  slug.value = "";
  slugTouched.value = false;
  error.value = "";
  slugError.value = "";
});
const canCreate = computed(() => title.value.trim() !== "" && slug.value !== "");

const create = async () => {
  if (!canCreate.value) return;
  busy.value = true;
  error.value = "";
  slugError.value = "";
  try {
    const post = await request<Post>("/api/posts", "POST", {
      title: title.value,
      slug: slug.value,
    });
    open.value = false;
    emit("created", post);
  } catch (cause) {
    if (cause instanceof ApiError && cause.code === "slug_taken")
      slugError.value = "Another post already uses this slug. Change it to continue.";
    else error.value = errorMessage(cause);
  } finally {
    busy.value = false;
  }
};
</script>

<template>
  <UModal v-model:open="open" title="New post" :ui="{ content: 'max-w-lg' }">
    <template #body>
      <form id="new-post" class="flex flex-col gap-5" @submit.prevent="create">
        <div class="field">
          <label for="new-title">Title</label>
          <UInput
            id="new-title"
            v-model="title"
            size="xl"
            autofocus
            required
            maxlength="300"
            placeholder="What is this post about?"
            class="w-full"
          />
        </div>
        <div class="field">
          <label for="new-slug">Slug</label>
          <UInput
            id="new-slug"
            v-model="slug"
            class="w-full mono"
            spellcheck="false"
            autocapitalize="none"
            maxlength="120"
            placeholder="Filled in from the title"
            :color="slugError ? 'error' : undefined"
            :aria-describedby="slugError ? 'new-slug-error' : 'new-slug-hint'"
            @input="
              slugTouched = true;
              slug = slugify(slug, true);
            "
          />
          <p v-if="slugError" id="new-slug-error" class="field-error">{{ slugError }}</p>
          <p v-else id="new-slug-hint" class="field-hint">
            Part of the post's address and its file name on your site.
          </p>
        </div>
        <p v-if="error" class="alert" role="alert">{{ error }}</p>
      </form>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" @click="open = false">Cancel</UButton>
        <UButton type="submit" form="new-post" :loading="busy" :disabled="!canCreate"
          >Create draft</UButton
        >
      </div>
    </template>
  </UModal>
</template>
