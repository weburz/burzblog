<script setup lang="ts">
import { ref } from "vue";

const props = defineProps<{ value: string; label: string; multiline?: boolean }>();
const copied = ref(false);
const failed = ref(false);
const copy = async () => {
  try {
    await navigator.clipboard.writeText(props.value);
    copied.value = true;
    failed.value = false;
    setTimeout(() => (copied.value = false), 2000);
  } catch {
    failed.value = true;
  }
};
</script>

<template>
  <div class="copy-field">
    <pre
      class="terminal"
      :class="{ 'terminal-wrap': !multiline }"
      role="group"
      tabindex="0"
      :aria-label="label"
    ><code>{{ value }}</code></pre>
    <UButton
      size="sm"
      color="neutral"
      variant="outline"
      :icon="copied ? 'i-lucide-check' : 'i-lucide-copy'"
      class="copy-button"
      :aria-label="`Copy ${label}`"
      @click="copy"
      >{{ copied ? "Copied" : "Copy" }}</UButton
    >
    <span class="sr-only" role="status">{{
      copied ? "Copied" : failed ? "Copy failed" : ""
    }}</span>
    <p v-if="failed" class="field-hint">
      Your browser blocked copying. Select the text and copy it instead.
    </p>
  </div>
</template>
