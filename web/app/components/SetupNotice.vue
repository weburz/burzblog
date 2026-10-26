<script setup lang="ts">
import { onMounted, ref } from "vue";
import { request } from "../utils/api";

// Tells a first-time visitor why sign-in cannot work yet. If the server cannot
// be asked, say nothing: the sign-in form reports connection problems itself.
const needed = ref(false);
onMounted(async () => {
  try {
    needed.value = (await request<{ needed: boolean }>("/api/setup")).needed;
  } catch {
    needed.value = false;
  }
});
</script>

<template>
  <p v-if="needed" class="notice" role="status">
    No accounts yet — open the setup link printed by
    <code class="mono">burzblog serve</code>, or run
    <code class="mono">burzblog user add &lt;name&gt;</code> on the host.
  </p>
</template>
