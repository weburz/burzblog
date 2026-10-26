<script setup lang="ts">
import { onMounted, ref } from "vue";
import type { TwoFactorStatus } from "../types";
import { errorMessage, request } from "../utils/api";
import { groupSecret } from "../utils/totp";

const toast = useToast();
const status = ref<TwoFactorStatus | null>(null);
const loadError = ref("");

// Setup: password, then the secret and a first code, then the recovery codes.
const step = ref<"idle" | "password" | "confirm" | "codes" | "disable">("idle");
const password = ref("");
const code = ref("");
const secret = ref("");
const uri = ref("");
const recovery = ref<string[]>([]);
const busy = ref(false);
const error = ref("");

const load = async () => {
  try {
    status.value = await request<TwoFactorStatus>("/api/account/2fa");
  } catch (cause) {
    loadError.value = errorMessage(cause);
  }
};
onMounted(load);

const reset = () => {
  step.value = "idle";
  password.value = code.value = secret.value = uri.value = error.value = "";
};

// Runs a request with the busy and error handling every step shares.
const attempt = async (work: () => Promise<void>) => {
  busy.value = true;
  error.value = "";
  try {
    await work();
  } catch (cause) {
    error.value = errorMessage(cause);
  } finally {
    busy.value = false;
  }
};

const start = () =>
  attempt(async () => {
    const reply = await request<{ secret: string; uri: string }>(
      "/api/account/2fa/start",
      "POST",
      { password: password.value },
    );
    secret.value = reply.secret;
    uri.value = reply.uri;
    password.value = "";
    step.value = "confirm";
  });

const confirm = () =>
  attempt(async () => {
    const reply = await request<{ recoveryCodes: string[] }>(
      "/api/account/2fa/confirm",
      "POST",
      { code: code.value.trim() },
    );
    recovery.value = reply.recoveryCodes;
    secret.value = uri.value = code.value = "";
    step.value = "codes";
    await load();
  });

const disable = () =>
  attempt(async () => {
    await request("/api/account/2fa/disable", "POST", {
      password: password.value,
      code: code.value.trim(),
    });
    reset();
    await load();
    toast.add({
      title: "Two-factor sign-in is off",
      description: "Other browsers were signed out.",
      color: "success",
    });
  });

const savedCodes = () => {
  recovery.value = [];
  reset();
};
</script>

<template>
  <section class="settings-section" aria-labelledby="two-factor-heading">
    <div class="settings-aside">
      <h2 id="two-factor-heading" class="section-title">Two-factor sign-in</h2>
      <p class="muted">
        Asks for a code from an authenticator app after your password, so a leaked
        password alone cannot sign in. Terminal logins that already ran
        <code>burzblog login</code> keep working.
      </p>
    </div>
    <div class="settings-main">
      <p v-if="loadError" class="alert" role="alert">{{ loadError }}</p>

      <template v-else-if="status">
        <div v-if="step === 'codes'" class="secret-reveal" role="alert">
          <p>
            <strong>Two-factor sign-in is on. Save these recovery codes now.</strong>
            They are shown only once. Each works one time if you lose your authenticator
            app.
          </p>
          <CopyField label="Recovery codes" multiline :value="recovery.join('\n')" />
          <UButton color="neutral" variant="ghost" size="sm" @click="savedCodes"
            >I have saved them</UButton
          >
        </div>

        <template v-else-if="status.enabled">
          <p class="deploy-title">
            <UIcon name="i-lucide-shield-check" class="size-4" />On.
            {{ status.recoveryCodes }} recovery
            {{ status.recoveryCodes === 1 ? "code" : "codes" }} left.
          </p>
          <form v-if="step === 'disable'" class="password-form" @submit.prevent="disable">
            <div class="field">
              <label for="tf-disable-password">Current password</label>
              <PasswordInput
                id="tf-disable-password"
                v-model="password"
                autocomplete="current-password"
                required
              />
            </div>
            <div class="field">
              <label for="tf-disable-code">Authentication or recovery code</label>
              <UInput
                id="tf-disable-code"
                v-model="code"
                inputmode="numeric"
                autocomplete="one-time-code"
                spellcheck="false"
                required
                class="w-full"
              />
            </div>
            <p v-if="error" class="alert" role="alert">{{ error }}</p>
            <div class="inline-form">
              <UButton type="submit" color="error" :loading="busy"
                >Turn off two-factor sign-in</UButton
              >
              <UButton color="neutral" variant="ghost" @click="reset">Cancel</UButton>
            </div>
          </form>
          <div v-else>
            <UButton
              color="neutral"
              variant="outline"
              icon="i-lucide-shield-off"
              @click="step = 'disable'"
              >Turn off</UButton
            >
          </div>
        </template>

        <form
          v-else-if="step === 'confirm'"
          class="password-form"
          @submit.prevent="confirm"
        >
          <p class="muted">
            Add this account to an authenticator app (1Password, Aegis, Google
            Authenticator and others) with the key below, then enter the code it shows.
          </p>
          <CopyField label="Setup key" :value="groupSecret(secret)" />
          <details class="field">
            <summary>Use an otpauth link instead</summary>
            <CopyField label="otpauth link" :value="uri" />
          </details>
          <div class="field">
            <label for="tf-code">Code from the app</label>
            <UInput
              id="tf-code"
              v-model="code"
              inputmode="numeric"
              autocomplete="one-time-code"
              spellcheck="false"
              required
              class="w-full"
            />
          </div>
          <p v-if="error" class="alert" role="alert">{{ error }}</p>
          <div class="inline-form">
            <UButton type="submit" :loading="busy">Turn on</UButton>
            <UButton color="neutral" variant="ghost" @click="reset">Cancel</UButton>
          </div>
        </form>

        <form
          v-else-if="step === 'password'"
          class="password-form"
          @submit.prevent="start"
        >
          <div class="field">
            <label for="tf-password">Current password</label>
            <PasswordInput
              id="tf-password"
              v-model="password"
              autocomplete="current-password"
              required
              autofocus
            />
          </div>
          <p v-if="error" class="alert" role="alert">{{ error }}</p>
          <div class="inline-form">
            <UButton type="submit" :loading="busy">Continue</UButton>
            <UButton color="neutral" variant="ghost" @click="reset">Cancel</UButton>
          </div>
        </form>

        <div v-else>
          <p class="muted">
            Off. Your password is the only thing protecting this account.
          </p>
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-shield"
            @click="step = 'password'"
            >Set up two-factor sign-in</UButton
          >
        </div>
      </template>
    </div>
  </section>
</template>
