<script setup lang="ts">
import { onMounted, ref } from "vue";
import type { TagCount, TagLabel } from "../types";
import { errorMessage, request } from "../utils/api";
import { maxTagLabel } from "../utils/metadata";

// Admins name tags for readers. The tag stays the slug on posts and in URLs;
// the label is what a website shows, and the server derives one when none is set.
const toast = useToast();
const tags = ref<TagCount[] | null>(null);
const loadError = ref("");
// What each label field holds, by tag, while it is being edited.
const edits = ref<Record<string, string>>({});
const busy = ref("");

const load = async () => {
  try {
    tags.value = (await request<{ items: TagCount[] }>("/api/tags")).items;
    edits.value = Object.fromEntries(tags.value.map((t) => [t.tag, t.label]));
  } catch (cause) {
    loadError.value = errorMessage(cause);
  }
};
onMounted(load);

const changed = (t: TagCount) => {
  const value = (edits.value[t.tag] ?? "").trim();
  return value !== "" && value !== t.label;
};

// Replaces one tag with the server's answer, keeping its post count.
const apply = (t: TagCount, saved: TagLabel) => {
  Object.assign(t, { label: saved.label, custom: saved.custom });
  edits.value[t.tag] = saved.label;
};

const save = async (t: TagCount) => {
  busy.value = t.tag;
  try {
    const saved = await request<TagLabel>(
      `/api/tags/${encodeURIComponent(t.tag)}/label`,
      "PUT",
      { label: edits.value[t.tag] },
    );
    apply(t, saved);
    toast.add({
      title: "Label saved",
      description: `${t.tag} now shows as ${saved.label}.`,
      color: "success",
    });
  } catch (cause) {
    toast.add({
      title: "Could not save the label",
      description: errorMessage(cause),
      color: "error",
    });
  } finally {
    busy.value = "";
  }
};

const reset = async (t: TagCount) => {
  busy.value = t.tag;
  try {
    const saved = await request<TagLabel>(
      `/api/tags/${encodeURIComponent(t.tag)}/label`,
      "DELETE",
    );
    apply(t, saved);
    toast.add({
      title: "Label reset",
      description: `${t.tag} shows as ${saved.label} again.`,
      color: "success",
    });
  } catch (cause) {
    toast.add({
      title: "Could not reset the label",
      description: errorMessage(cause),
      color: "error",
    });
  } finally {
    busy.value = "";
  }
};
</script>

<template>
  <section class="settings-section" aria-labelledby="tags-heading">
    <div class="settings-aside">
      <h2 id="tags-heading" class="section-title">Tag labels</h2>
      <p class="muted">
        Tags are saved as lowercase words with hyphens. Give a tag a readable label for
        your website's menus and tag pages. Without one, the label is made from the tag:
        <span class="mono">constitutional-law</span> shows as Constitutional Law.
      </p>
    </div>
    <div class="settings-main">
      <p v-if="loadError" class="alert" role="alert">{{ loadError }}</p>
      <p v-else-if="tags && !tags.length" class="muted">
        No tags yet. Add tags in a post's settings and they appear here.
      </p>
      <ul v-else-if="tags" class="token-list">
        <li v-for="t in tags" :key="t.tag">
          <form class="tag-label-row" @submit.prevent="save(t)">
            <div class="tag-label-slug">
              <span class="mono">{{ t.tag }}</span>
              <span class="muted"
                >{{ t.count }} {{ t.count === 1 ? "post" : "posts" }}</span
              >
            </div>
            <div class="field">
              <label :for="`tag-label-${t.tag}`">Label for {{ t.tag }}</label>
              <UInput
                :id="`tag-label-${t.tag}`"
                v-model="edits[t.tag]"
                class="w-full"
                :maxlength="maxTagLabel"
                autocomplete="off"
                required
              />
            </div>
            <div class="tag-label-actions">
              <UButton
                type="submit"
                color="secondary"
                :loading="busy === t.tag"
                :disabled="!changed(t)"
                >Save</UButton
              >
              <UButton
                v-if="t.custom"
                color="neutral"
                variant="ghost"
                :disabled="busy === t.tag"
                @click="reset(t)"
                >Reset</UButton
              >
            </div>
          </form>
        </li>
      </ul>
    </div>
  </section>
</template>
