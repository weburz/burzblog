<script setup lang="ts">
import { computed } from "vue";
import type { PostDraft, PostStatus } from "../types";
import { slugify } from "../utils/slug";
import { fromLocalInput, toLocalInput } from "../utils/time";

const open = defineModel<boolean>("open", { default: false });
const draft = defineModel<PostDraft>("draft", { required: true });
const props = defineProps<{
  status: PostStatus;
  savedSlug: string;
  readonly?: boolean;
  closeFocus?: (event: Event) => void;
}>();

const published = computed({
  get: () => toLocalInput(draft.value.publishedOn),
  set: (v: string) => (draft.value.publishedOn = fromLocalInput(v)),
});
const updated = computed({
  get: () => toLocalInput(draft.value.updatedOn),
  set: (v: string) => (draft.value.updatedOn = fromLocalInput(v)),
});
const coverOk = computed(() => /^https?:\/\/\S+$/.test(draft.value.coverUrl));
const slugChanged = computed(() => draft.value.slug !== props.savedSlug);
const setUpdatedNow = () => {
  draft.value.updatedOn = new Date().toISOString().replace(/\.\d{3}Z$/, "Z");
};
</script>

<template>
  <USlideover
    v-model:open="open"
    title="Post settings"
    description="How the post is addressed and presented on your site."
    :ui="{ content: 'max-w-md' }"
    :content="{ onCloseAutoFocus: closeFocus }"
  >
    <template #body>
      <fieldset class="details" :disabled="readonly">
        <div class="field">
          <label for="detail-slug">Slug</label>
          <UInput
            id="detail-slug"
            :model-value="draft.slug"
            class="w-full mono"
            spellcheck="false"
            autocapitalize="none"
            maxlength="120"
            aria-describedby="detail-slug-hint"
            @update:model-value="
              (v: string | number) => (draft.slug = slugify(String(v), true))
            "
          />
          <p id="detail-slug-hint" class="field-hint">
            <template v-if="slugChanged && status === 'published'">
              This post is live. A new slug gives it a new address, and links to
              <span class="mono">{{ savedSlug }}</span> will stop working.
            </template>
            <template v-else
              >Lowercase words joined by hyphens. Also the file name readers' sites
              use.</template
            >
          </p>
        </div>

        <div class="field">
          <label for="detail-cover">Cover image URL</label>
          <UInput
            id="detail-cover"
            v-model="draft.coverUrl"
            type="url"
            inputmode="url"
            placeholder="https://…"
            class="w-full"
          />
          <img
            v-if="coverOk"
            :src="draft.coverUrl"
            :alt="draft.coverAlt"
            class="cover-thumb"
            referrerpolicy="no-referrer"
          />
        </div>
        <div v-if="draft.coverUrl" class="field">
          <label for="detail-alt">Cover description</label>
          <UInput
            id="detail-alt"
            v-model="draft.coverAlt"
            class="w-full"
            maxlength="500"
            aria-describedby="detail-alt-hint"
          />
          <p id="detail-alt-hint" class="field-hint">
            What the image shows, for people who cannot see it.
          </p>
        </div>

        <div class="field">
          <label for="detail-published">Published on</label>
          <UInput
            id="detail-published"
            v-model="published"
            type="datetime-local"
            class="w-full"
          />
          <p class="field-hint">Set automatically the first time you publish.</p>
        </div>
        <div class="field">
          <label for="detail-updated">Last revised</label>
          <div class="flex gap-2">
            <UInput
              id="detail-updated"
              v-model="updated"
              type="datetime-local"
              class="flex-1"
            />
            <UButton color="neutral" variant="outline" @click="setUpdatedNow"
              >Now</UButton
            >
          </div>
          <p class="field-hint">
            Optional. Shown to readers when you have made a meaningful update.
          </p>
        </div>

        <div class="field">
          <label for="detail-extra">Extra frontmatter</label>
          <textarea
            id="detail-extra"
            v-model="draft.extra"
            class="extra-source mono"
            rows="6"
            spellcheck="false"
            placeholder="sitemap:&#10;  changefreq: yearly"
            aria-describedby="detail-extra-hint"
          />
          <p id="detail-extra-hint" class="field-hint">
            YAML for anything else your site reads, such as
            <span class="mono">sitemap</span> or <span class="mono">seo</span>. Kept
            exactly as written.
          </p>
        </div>
      </fieldset>
    </template>
  </USlideover>
</template>
