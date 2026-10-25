<script setup lang="ts">
useHead({ title: "Page not found" });
const route = useRoute();
const paletteOpen = useState("palette-open", () => false);
</script>

<template>
  <div class="content">
    <div class="page-head">
      <div>
        <h1 class="page-title">Page not found</h1>
        <p class="muted">
          Nothing lives at <span class="mono">{{ route.path }}</span
          >. The link may be old, or the post may have been renamed or removed.
        </p>
      </div>
    </div>
    <div class="load-error-actions">
      <UButton to="/" icon="i-lucide-arrow-left">All posts</UButton>
      <UButton
        color="neutral"
        variant="outline"
        icon="i-lucide-search"
        aria-keyshortcuts="Control+K Meta+K"
        @click="paletteOpen = true"
      >
        Search for a post
        <UKbd value="meta" class="max-sm:hidden" /><UKbd
          value="k"
          class="max-sm:hidden"
        />
      </UButton>
    </div>
    <p class="list-foot muted">
      Search matches titles and slugs, and also jumps to settings and shortcuts.
    </p>
  </div>
</template>
