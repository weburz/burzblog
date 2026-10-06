<script setup lang="ts">
import { ref } from "vue";
import { request, errorMessage } from "../utils/api";
import { withRetainedFocus } from "../utils/focus";

defineProps<{ compact?: boolean }>();
const emit = defineEmits<{ success: [] }>();
const password = ref("");
const busy = ref(false);
const error = ref("");
const submit = () =>
  withRetainedFocus(async () => {
    busy.value = true;
    error.value = "";
    try {
      await request("/api/login", "POST", { password: password.value });
      password.value = "";
      emit("success");
    } catch (cause) {
      error.value = errorMessage(cause);
    } finally {
      busy.value = false;
    }
  });
</script>

<template>
  <form
    class="login"
    :class="{ 'login-compact': compact, panel: !compact }"
    @submit.prevent="submit"
  >
    <div v-if="!compact">
      <h1 class="login-title mb-3">A quiet place to write.</h1>
      <p class="muted">Your words, in Markdown files you own.</p>
    </div>
    <div class="field !mb-0 login-field">
      <label for="password">Administrator password</label>
      <UInput
        id="password"
        v-model="password"
        type="password"
        autocomplete="current-password"
        required
        autofocus
        class="w-full"
        :size="compact ? 'lg' : 'xl'"
        :aria-describedby="error ? 'login-error' : undefined"
      />
    </div>
    <p v-if="error" id="login-error" class="alert login-error !my-0" role="alert">
      {{ error }}
    </p>
    <div class="login-actions">
      <UButton
        type="submit"
        color="primary"
        size="lg"
        class="justify-center"
        :class="{ 'w-full': !compact }"
        :loading="busy"
        >Sign in</UButton
      >
      <slot name="actions" />
    </div>
  </form>
</template>
