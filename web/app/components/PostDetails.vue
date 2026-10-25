<script setup lang="ts">
import { computed, ref } from "vue";
import type { PostDraft, PostStatus } from "../types";
import {
  canonicalOk,
  counterLabel,
  maxAuthor,
  maxTags,
  normalizeTags,
  overAdvice,
  seoDescriptionLimits,
  seoTitleLimits,
} from "../utils/metadata";
import { slugify } from "../utils/slug";
import { fromLocalInput, toLocalInput } from "../utils/time";

const open = defineModel<boolean>("open", { default: false });
const draft = defineModel<PostDraft>("draft", { required: true });
const props = defineProps<{
  status: PostStatus;
  savedSlug: string;
  readonly?: boolean;
  // Old slugs of this post that redirect to it.
  redirects?: string[];
  closeFocus?: (event: Event) => void;
}>();
const emit = defineEmits<{ removeRedirect: [from: string] }>();

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
const canonicalValid = computed(() => canonicalOk(draft.value.canonicalUrl));
// The SEO section opens by itself when the post already has SEO settings.
const seoOpen = ref(
  Boolean(draft.value.seoTitle || draft.value.seoDescription || draft.value.canonicalUrl),
);
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
              This post is live. A new slug gives it a new address. The old address
              <span class="mono">{{ savedSlug }}</span> will redirect here.
            </template>
            <template v-else
              >Lowercase words joined by hyphens. Also the file name readers' sites
              use.</template
            >
          </p>
          <div v-if="redirects?.length" class="redirects">
            <p id="detail-redirects-label" class="field-hint">
              These old addresses redirect to this post:
            </p>
            <ul aria-labelledby="detail-redirects-label">
              <li v-for="from in redirects" :key="from">
                <span class="mono">{{ from }}</span>
                <UButton
                  color="neutral"
                  variant="ghost"
                  size="xs"
                  icon="i-lucide-x"
                  :aria-label="`Remove the redirect from ${from}`"
                  @click="emit('removeRedirect', from)"
                />
              </li>
            </ul>
          </div>
        </div>

        <div class="field">
          <label for="detail-tags">Tags</label>
          <UInputTags
            id="detail-tags"
            :model-value="draft.tags"
            :max="maxTags"
            add-on-blur
            placeholder="Add a tag, then press Enter"
            class="w-full"
            aria-describedby="detail-tags-hint"
            @update:model-value="(v: string[]) => (draft.tags = normalizeTags(v))"
          />
          <p id="detail-tags-hint" class="field-hint">
            Topics and categories in one list. Lowercase words joined by hyphens, up to
            {{ maxTags }} tags.
          </p>
        </div>

        <div class="field">
          <label for="detail-author">Author</label>
          <UInput
            id="detail-author"
            v-model="draft.author"
            class="w-full"
            :maxlength="maxAuthor"
            autocomplete="off"
            aria-describedby="detail-author-hint"
          />
          <p id="detail-author-hint" class="field-hint">
            The name readers see. New posts start with the account that created them.
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

        <UCollapsible v-model:open="seoOpen" class="seo-section">
          <UButton
            color="neutral"
            variant="ghost"
            class="seo-toggle"
            trailing-icon="i-lucide-chevron-down"
            :aria-expanded="seoOpen"
            aria-controls="seo-fields"
            >SEO</UButton
          >
          <template #content>
            <div id="seo-fields" class="details seo-fields">
              <div class="field">
                <label for="detail-seo-title">SEO title</label>
                <UInput
                  id="detail-seo-title"
                  v-model="draft.seoTitle"
                  class="w-full"
                  :maxlength="seoTitleLimits.max"
                  aria-describedby="detail-seo-title-hint"
                />
                <p
                  id="detail-seo-title-hint"
                  class="field-hint counter"
                  :class="{
                    'counter-over': overAdvice(draft.seoTitle, seoTitleLimits.advised),
                  }"
                >
                  <span aria-hidden="true">{{
                    counterLabel(draft.seoTitle, seoTitleLimits.advised)
                  }}</span>
                  Shown in search results instead of the title. Search engines usually cut
                  it off after about {{ seoTitleLimits.advised }} characters.
                </p>
              </div>
              <div class="field">
                <label for="detail-seo-description">SEO description</label>
                <textarea
                  id="detail-seo-description"
                  v-model="draft.seoDescription"
                  class="extra-source"
                  rows="3"
                  :maxlength="seoDescriptionLimits.max"
                  aria-describedby="detail-seo-description-hint"
                  @keydown.enter.prevent
                />
                <p
                  id="detail-seo-description-hint"
                  class="field-hint counter"
                  :class="{
                    'counter-over': overAdvice(
                      draft.seoDescription,
                      seoDescriptionLimits.advised,
                    ),
                  }"
                >
                  <span aria-hidden="true">{{
                    counterLabel(draft.seoDescription, seoDescriptionLimits.advised)
                  }}</span>
                  Shown under the title in search results. Aim for about
                  {{ seoDescriptionLimits.advised }} characters.
                </p>
              </div>
              <div class="field">
                <label for="detail-canonical">Canonical URL</label>
                <UInput
                  id="detail-canonical"
                  v-model="draft.canonicalUrl"
                  type="url"
                  inputmode="url"
                  placeholder="https://…"
                  class="w-full"
                  :aria-invalid="!canonicalValid"
                  aria-describedby="detail-canonical-hint"
                />
                <p v-if="!canonicalValid" id="detail-canonical-hint" class="field-error">
                  Use a full https address, such as https://example.com/my-post.
                </p>
                <p v-else id="detail-canonical-hint" class="field-hint">
                  Only when the post also lives at another address that search engines
                  should prefer.
                </p>
              </div>
            </div>
          </template>
        </UCollapsible>

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
            <span class="mono">sitemap</span>. Kept exactly as written.
          </p>
        </div>
      </fieldset>
    </template>
  </USlideover>
</template>
