<script setup lang="ts">
withDefaults(
  defineProps<{
    title: string;
    body: string;
    confirmLabel: string;
    confirmColor?: "primary" | "error" | "neutral";
    closeFocus?: (event: Event) => void;
  }>(),
  { confirmColor: "primary" },
);
const open = defineModel<boolean>("open", { default: false });
const emit = defineEmits<{ confirm: [] }>();
</script>

<template>
  <UModal
    v-model:open="open"
    :title="title"
    :description="body"
    :content="{ onCloseAutoFocus: closeFocus }"
  >
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" autofocus @click="open = false"
          >Cancel</UButton
        >
        <UButton :color="confirmColor" @click="emit('confirm')">{{
          confirmLabel
        }}</UButton>
      </div>
    </template>
  </UModal>
</template>
