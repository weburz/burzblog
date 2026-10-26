<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { errorMessage, request } from "../utils/api";
import { minPassword, passwordProblem, setupTokenFromHash } from "../utils/setup";

useHead({ title: "Create the first account" });
const { createFirstAccount } = useCurrentSession();

const ready = ref(false);
// Held in memory only; the address bar is cleaned as soon as the page opens.
const token = ref("");
const name = ref("");
const password = ref("");
const repeat = ref("");
const busy = ref(false);
const error = ref("");

onMounted(async () => {
  token.value = setupTokenFromHash(window.location.hash);
  if (window.location.hash)
    window.history.replaceState(
      window.history.state,
      "",
      window.location.pathname + window.location.search,
    );
  try {
    const { needed } = await request<{ needed: boolean }>("/api/setup");
    if (!needed) {
      await navigateTo("/", { replace: true });
      return;
    }
  } catch (cause) {
    error.value = errorMessage(cause);
  }
  ready.value = true;
});

const mismatch = computed(() => repeat.value !== "" && password.value !== repeat.value);
const submit = async () => {
  const problem = passwordProblem(password.value, repeat.value);
  if (problem) {
    error.value = problem;
    return;
  }
  busy.value = true;
  error.value = "";
  try {
    await createFirstAccount(token.value, name.value.trim(), password.value);
    password.value = repeat.value = "";
    await navigateTo("/", { replace: true });
  } catch (cause) {
    error.value = errorMessage(cause);
  } finally {
    busy.value = false;
  }
};
</script>

<template>
  <div class="content">
    <p v-if="!ready" class="loading" role="status">Checking this server…</p>
    <div v-else-if="!token" class="login">
      <h1 class="login-title">Create the first account</h1>
      <p v-if="error" class="alert" role="alert">{{ error }}</p>
      <p class="muted">
        Open the setup link printed by <code class="mono">burzblog serve</code> (in
        <code class="mono">docker logs</code> or <code class="mono">journalctl</code>), or
        run <code class="mono">burzblog user add &lt;name&gt;</code> on the host.
      </p>
    </div>
    <form v-else class="login" @submit.prevent="submit">
      <div class="login-intro">
        <h1 class="login-title">Create the first account</h1>
        <p class="muted">
          This account can write, publish and sign others in. You only do this once.
        </p>
      </div>
      <div class="field">
        <label for="setup-username">Username</label>
        <UInput
          id="setup-username"
          v-model="name"
          autocomplete="username"
          autocapitalize="none"
          spellcheck="false"
          required
          maxlength="64"
          autofocus
          class="w-full"
          size="xl"
        />
      </div>
      <div class="field">
        <label for="setup-password">Password</label>
        <PasswordInput
          id="setup-password"
          v-model="password"
          autocomplete="new-password"
          required
          :minlength="minPassword"
          describedby="setup-password-hint"
        />
        <p id="setup-password-hint" class="field-hint">
          At least {{ minPassword }} characters. A few unrelated words work well.
        </p>
      </div>
      <div class="field">
        <label for="setup-repeat">Repeat password</label>
        <PasswordInput
          id="setup-repeat"
          v-model="repeat"
          autocomplete="new-password"
          required
          :invalid="mismatch"
          :describedby="mismatch ? 'setup-repeat-error' : undefined"
        />
        <p v-if="mismatch" id="setup-repeat-error" class="field-error">
          The two passwords are different.
        </p>
      </div>
      <p v-if="error" class="alert" role="alert">{{ error }}</p>
      <div class="login-actions">
        <UButton
          type="submit"
          color="primary"
          size="xl"
          class="w-full justify-center"
          :loading="busy"
          >Create account</UButton
        >
      </div>
    </form>
  </div>
</template>
