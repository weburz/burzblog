<script setup lang="ts">
import { ref } from "vue";
import { request, errorMessage } from "../utils/api";

const emit = defineEmits<{ success: [] }>();
const password = ref("");
const busy = ref(false);
const error = ref("");
const submit = async () => {
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
};
</script>

<template>
  <form class="panel max-w-sm" @submit.prevent="submit">
    <h1 class="page-title mb-3">A quiet place to write.</h1>
    <p class="muted mb-8">Your words. Your Markdown. Your files.</p>
    <div class="field">
      <label for="password">Administrator password</label>
      <UInput
        id="password"
        v-model="password"
        type="password"
        autocomplete="current-password"
        required
        class="w-full"
        size="xl"
        :aria-describedby="error ? 'login-error' : undefined"
      />
    </div>
    <p v-if="error" id="login-error" class="error" role="alert">{{ error }}</p>
    <UButton type="submit" color="secondary" size="lg" :loading="busy">Sign in</UButton>
  </form>
</template>
