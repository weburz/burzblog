<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from "vue";

defineProps<{
  id: string;
  autocomplete: string;
  required?: boolean;
  minlength?: number | string;
  invalid?: boolean;
  describedby?: string;
  autofocus?: boolean;
}>();
const model = defineModel<string>({ default: "" });

const root = ref<HTMLElement | null>(null);
const visible = ref(false);
const capsLock = ref(false);

const field = () => root.value?.querySelector<HTMLInputElement>("input") ?? null;

const toggle = async () => {
  const input = field();
  const start = input?.selectionStart ?? null;
  const end = input?.selectionEnd ?? null;
  visible.value = !visible.value;
  await nextTick();
  const next = field();
  if (!next) return;
  next.focus();
  if (start !== null && end !== null) next.setSelectionRange(start, end);
};

const updateCapsLock = (event: KeyboardEvent) => {
  if (typeof event.getModifierState === "function") {
    capsLock.value = event.getModifierState("CapsLock");
  }
};

// A submitted password should not stay on screen, even if sign-in fails.
const hide = () => {
  visible.value = false;
};
let form: HTMLFormElement | null = null;
onMounted(() => {
  form = root.value?.closest("form") ?? null;
  form?.addEventListener("submit", hide);
});
onBeforeUnmount(() => form?.removeEventListener("submit", hide));

defineExpose({ hide });
</script>

<template>
  <div
    ref="root"
    class="password-input"
    @keydown="updateCapsLock"
    @keyup="updateCapsLock"
    @focusout="capsLock = false"
  >
    <UInput
      :id="id"
      v-model="model"
      :type="visible ? 'text' : 'password'"
      :autocomplete="autocomplete"
      :required="required"
      :minlength="minlength"
      :autofocus="autofocus"
      :color="invalid ? 'error' : undefined"
      :aria-describedby="[describedby, `${id}-caps`].filter(Boolean).join(' ')"
      class="w-full"
      size="xl"
    >
      <template #trailing>
        <UButton
          :id="`${id}-toggle`"
          type="button"
          color="neutral"
          variant="link"
          size="sm"
          :icon="visible ? 'i-lucide-eye-off' : 'i-lucide-eye'"
          :aria-label="visible ? 'Hide password' : 'Show password'"
          :aria-pressed="visible"
          :aria-controls="id"
          @mousedown.prevent
          @click="toggle"
        />
      </template>
    </UInput>
    <p :id="`${id}-caps`" class="field-hint" role="status" aria-live="polite">
      {{ capsLock ? "Caps Lock is on" : "" }}
    </p>
  </div>
</template>
