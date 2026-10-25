<script setup lang="ts">
import { computed } from "vue";
import type { PostStatus } from "../types";
import { isScheduled } from "../utils/time";

// A draft with a publish time reads as Scheduled.
const props = defineProps<{ status: PostStatus; publishAt?: string | null }>();
const kind = computed(() =>
  isScheduled({ status: props.status, publishAt: props.publishAt })
    ? "scheduled"
    : props.status,
);
const label = computed(
  () =>
    ({
      draft: "Draft",
      scheduled: "Scheduled",
      published: "Published",
      archived: "Archived",
    })[kind.value],
);
</script>

<template>
  <span class="badge" :class="`badge-${kind}`">
    <span class="badge-dot" aria-hidden="true" />{{ label }}
  </span>
</template>
