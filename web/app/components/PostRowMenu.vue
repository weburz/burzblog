<script setup lang="ts">
import { computed } from "vue";
import type { PostSummary } from "../types";
import { actionApplies, type PostAction } from "../utils/post-actions";

// `tabbable` false takes the button out of the Tab order (the list keeps one
// stop per row); it still opens with the pointer and from the focused row.
const props = withDefaults(
  defineProps<{ post: PostSummary; admin: boolean; tabbable?: boolean }>(),
  { tabbable: true },
);
const emit = defineEmits<{
  act: [action: PostAction];
  duplicate: [];
  copyLink: [];
}>();

const label = computed(() => `Actions for ${props.post.title || props.post.slug}`);

const items = computed(() => {
  const item = (action: PostAction, text: string, icon: string, color?: "error") => ({
    label: text,
    icon,
    ...(color && { color }),
    onSelect: () => emit("act", action),
  });
  const applies = (action: PostAction) => actionApplies(action, props.post, props.admin);
  const status = [
    applies("publish") && item("publish", "Publish", "i-lucide-send"),
    applies("unpublish") && item("unpublish", "Unpublish", "i-lucide-eye-off"),
    applies("archive") && item("archive", "Archive", "i-lucide-archive"),
    applies("restore") && item("restore", "Restore as draft", "i-lucide-archive-restore"),
  ].filter((entry) => entry !== false);
  const more = [
    { label: "Duplicate", icon: "i-lucide-copy", onSelect: () => emit("duplicate") },
    ...(props.post.status === "published"
      ? [{ label: "Copy link", icon: "i-lucide-link", onSelect: () => emit("copyLink") }]
      : []),
  ];
  const danger = applies("delete")
    ? [[item("delete", "Delete post…", "i-lucide-trash-2", "error")]]
    : [];
  return [...(status.length ? [status] : []), more, ...danger];
});
</script>

<template>
  <UDropdownMenu :items="items" :content="{ align: 'end' }">
    <UButton
      icon="i-lucide-ellipsis"
      color="neutral"
      variant="ghost"
      class="post-menu"
      :aria-label="label"
      :tabindex="tabbable ? undefined : -1"
    />
  </UDropdownMenu>
</template>
