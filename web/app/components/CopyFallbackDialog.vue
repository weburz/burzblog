<script setup lang="ts">
// Shown when the browser refuses to copy. It stays open until dismissed, with
// the text selectable, unlike a toast.
defineProps<{ title: string; value: string; label: string }>();
const open = defineModel<boolean>("open", { default: false });
</script>

<template>
  <UModal
    v-model:open="open"
    :title="title"
    description="Your browser did not allow copying. Select the text below and copy it."
  >
    <template #body>
      <CopyField :value="value" :label="label" />
    </template>
  </UModal>
</template>
