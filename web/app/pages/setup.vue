<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { ApiError, errorMessage, request } from "../utils/api";
import { minPassword, passwordProblem, setupTokenFromHash } from "../utils/setup";

useHead({ title: "Create the first account" });
const { createFirstAccount } = useCurrentSession();

const toast = useToast();
const ready = ref(false);
// Held in memory only; the address bar is cleaned as soon as the page opens.
const token = ref("");
const name = ref("");
const password = ref("");
const repeat = ref("");
const busy = ref(false);
const error = ref("");

const readSetupLink = () => {
  if (!window.location.hash) return;
  error.value = "";
  token.value = setupTokenFromHash(window.location.hash);
  window.history.replaceState(
    window.history.state,
    "",
    window.location.pathname + window.location.search,
  );
};
onUnmounted(() => window.removeEventListener("hashchange", readSetupLink));
onMounted(async () => {
  readSetupLink();
  window.addEventListener("hashchange", readSetupLink);
  try {
    const { needed } = await request<{ needed: boolean }>("/api/setup");
    if (!needed) {
      toast.add({ title: "Setup is complete. This server already has an account." });
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
  const submittedToken = token.value;
  busy.value = true;
  error.value = "";
  try {
    await createFirstAccount(submittedToken, name.value.trim(), password.value);
    password.value = repeat.value = "";
    await navigateTo("/", { replace: true });
  } catch (cause) {
    if (token.value !== submittedToken) return;
    if (cause instanceof ApiError && cause.code === "setup_token") token.value = "";
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
          This is the admin account. Add more people later with
          <code>burzblog user add</code> on the server.
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
          maxlength="128"
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
