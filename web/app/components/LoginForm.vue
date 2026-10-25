<script setup lang="ts">
import { computed, nextTick, ref } from "vue";
import { errorMessage } from "../utils/api";

const props = defineProps<{ compact?: boolean; username?: string }>();
const emit = defineEmits<{ success: [] }>();
const { signIn } = useCurrentSession();
const name = ref(props.username ?? "");
const password = ref("");
const resetCommand = computed(
  () => `burzblog user passwd ${name.value.trim() || "<name>"}`,
);
const busy = ref(false);
const error = ref("");

const submit = async () => {
  busy.value = true;
  error.value = "";
  try {
    await signIn(name.value.trim(), password.value);
    password.value = "";
    emit("success");
  } catch (cause) {
    error.value = errorMessage(cause);
    // Put the writer straight back into the password to retype it.
    await nextTick();
    const field = document.getElementById("login-password") as HTMLInputElement | null;
    field?.focus();
    field?.select();
  } finally {
    busy.value = false;
  }
};
</script>

<template>
  <form class="login" :class="{ 'login-compact': compact }" @submit.prevent="submit">
    <div v-if="!compact" class="login-intro">
      <h1 class="login-title">A quiet place to write.</h1>
      <p class="muted">
        Sign in to write in the browser. Prefer your own editor? Run
        <code class="mono">burzblog login</code> in a terminal.
      </p>
    </div>
    <div class="field">
      <label for="login-username">Username</label>
      <UInput
        id="login-username"
        v-model="name"
        autocomplete="username"
        autocapitalize="none"
        spellcheck="false"
        required
        :autofocus="!props.username"
        class="w-full"
        size="xl"
      />
    </div>
    <div class="field">
      <label for="login-password">Password</label>
      <PasswordInput
        id="login-password"
        v-model="password"
        autocomplete="current-password"
        required
        :autofocus="Boolean(props.username)"
        :describedby="error ? 'login-error' : undefined"
      />
    </div>
    <details class="login-forgot">
      <summary>Forgot your password?</summary>
      <p class="muted">
        BurzBlog has no email, so accounts are managed on the server. Ask whoever runs
        BurzBlog to run this command; it also signs the account out everywhere.
      </p>
      <CopyField :value="resetCommand" label="the password reset command" />
    </details>
    <p v-if="error" id="login-error" class="alert" role="alert">{{ error }}</p>
    <div class="login-actions">
      <UButton
        type="submit"
        color="primary"
        size="xl"
        class="justify-center"
        :class="{ 'w-full': !compact }"
        :loading="busy"
        >Sign in</UButton
      >
      <slot name="actions" />
    </div>
  </form>
</template>
