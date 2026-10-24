<script setup lang="ts">
import { computed } from "vue";
import type { PostStatus } from "../types";

const props = defineProps<{ status: PostStatus }>();
const label = computed(
  () => ({ draft: "Draft", published: "Published", archived: "Archived" })[props.status],
);
</script>

<template>
  <span class="badge" :class="`badge-${status}`">
    <span class="badge-dot" aria-hidden="true" />{{ label }}
  </span>
</template>
