<script setup lang="ts">
withDefaults(
  defineProps<{
    title: string;
    body: string;
    confirmLabel: string;
    confirmColor?: "primary" | "error" | "neutral";
    busy?: boolean;
  }>(),
  { confirmColor: "primary", busy: false },
);
const open = defineModel<boolean>("open", { default: false });
const emit = defineEmits<{ confirm: [] }>();
</script>

<template>
  <UModal
    v-model:open="open"
    :title="title"
    :description="body"
    :close="false"
    :dismissible="!busy"
  >
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton
          color="neutral"
          variant="ghost"
          autofocus
          :disabled="busy"
          @click="open = false"
          >Cancel</UButton
        >
        <UButton :color="confirmColor" :loading="busy" @click="emit('confirm')">{{
          confirmLabel
        }}</UButton>
      </div>
    </template>
  </UModal>
</template>
