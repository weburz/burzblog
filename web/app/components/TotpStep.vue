<script setup lang="ts">
import { ref } from "vue";

// The second step of signing in: the password was right, now the code.
defineProps<{ busy?: boolean; error?: string; compact?: boolean }>();
const emit = defineEmits<{ submit: [code: string]; restart: [] }>();
const code = ref("");
</script>

<template>
  <form
    class="login"
    :class="{ 'login-compact': compact }"
    @submit.prevent="emit('submit', code.trim())"
  >
    <div v-if="!compact" class="login-intro">
      <h1 class="login-title">One more step.</h1>
    </div>
    <div class="field">
      <label for="totp-code">Authentication code</label>
      <UInput
        id="totp-code"
        v-model="code"
        autocomplete="one-time-code"
        autocapitalize="none"
        spellcheck="false"
        required
        autofocus
        class="w-full"
        size="xl"
        aria-describedby="totp-hint"
      />
      <p id="totp-hint" class="field-hint">
        Enter the 6-digit code from your authenticator app. Lost your phone? Enter one of
        your recovery codes instead.
      </p>
    </div>
    <p v-if="error" id="totp-error" class="alert" role="alert">{{ error }}</p>
    <div class="login-actions">
      <UButton
        type="submit"
        color="primary"
        size="xl"
        class="justify-center"
        :class="{ 'w-full': !compact }"
        :loading="busy"
        >Verify</UButton
      >
      <UButton color="neutral" variant="ghost" size="xl" @click="emit('restart')"
        >Back</UButton
      >
    </div>
  </form>
</template>
