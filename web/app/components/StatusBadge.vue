<script setup lang="ts">
import { computed } from "vue";

const props = defineProps<{ status: "draft" | "published" | "unavailable" }>();
const label = computed(
  () =>
    ({ draft: "Draft", published: "Published", unavailable: "Needs attention" })[
      props.status
    ],
);
</script>

<template>
  <span class="badge" :class="`badge-${status}`">
    <UIcon v-if="status === 'published'" name="i-lucide-check" class="size-3" />
    <UIcon
      v-else-if="status === 'unavailable'"
      name="i-lucide-triangle-alert"
      class="size-3"
    />
    {{ label }}
  </span>
</template>
