<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { errorMessage } from "./utils/api";
import { themeOptions } from "./utils/theme";

const { state, load, signOut } = useCurrentSession();
const theme = usePreferredTheme();
let stopTheme = () => {};
onMounted(() => (stopTheme = theme.start()));
onUnmounted(() => stopTheme());
// The editor reports unsaved writing here, so signing out can warn first.
const dirty = useState("editor-dirty", () => false);
const editorFailed = useState("editor-failed", () => false);
const shortcutsOpen = useState("shortcuts-open", () => false);
const route = useRoute();
const error = ref("");
const confirmingSignOut = ref(false);

onMounted(async () => {
  try {
    await load();
  } catch (cause) {
    error.value = errorMessage(cause);
    state.value.ready = true;
  }
});

const expired = () => {
  if (state.value.authenticated) state.value.expired = true;
};
onMounted(() => window.addEventListener("burzblog:session-expired", expired));
onUnmounted(() => window.removeEventListener("burzblog:session-expired", expired));

// "?" opens the shortcut list, unless the writer is typing in a field.
const helpKey = (event: KeyboardEvent) => {
  if (event.key !== "?" || event.metaKey || event.ctrlKey || event.altKey) return;
  if (!state.value.authenticated) return;
  const target = event.target as HTMLElement | null;
  if (target?.closest("input, textarea, select, [contenteditable], [role=dialog]"))
    return;
  event.preventDefault();
  shortcutsOpen.value = true;
};
onMounted(() => window.addEventListener("keydown", helpKey));
onUnmounted(() => window.removeEventListener("keydown", helpKey));

const leave = async () => {
  confirmingSignOut.value = false;
  dirty.value = false;
  await signOut();
  await navigateTo("/");
};
const requestSignOut = () => {
  if (dirty.value) confirmingSignOut.value = true;
  else void leave();
};

const accountItems = computed(() => [
  [{ label: `Signed in as ${state.value.username}`, type: "label" as const }],
  [
    { label: "Settings", icon: "i-lucide-settings", to: "/settings" },
    {
      label: "Theme",
      icon: theme.dark.value ? "i-lucide-moon" : "i-lucide-sun",
      children: themeOptions.map((option) => ({
        label: option.label,
        icon: option.icon,
        type: "checkbox" as const,
        checked: theme.preference.value === option.value,
        onUpdateChecked: () => (theme.preference.value = option.value),
      })),
    },
  ],
  [{ label: "Sign out", icon: "i-lucide-log-out", onSelect: requestSignOut }],
]);
// The editor brings its own bar, unless the post could not be opened.
const editing = computed(() => route.path.startsWith("/posts/") && !editorFailed.value);

useHead({
  titleTemplate: (title) => (title ? `${title} · BurzBlog` : "BurzBlog"),
});
</script>

<template>
  <UApp :toaster="{ position: 'bottom-right' }">
    <div class="shell">
      <a href="#main" class="skip-link">Skip to content</a>
      <header v-if="!editing || !state.authenticated" class="topbar">
        <div class="topbar-inner">
          <NuxtLink to="/" class="brand" aria-label="BurzBlog, all posts">
            <span class="brand-mark" aria-hidden="true" />BurzBlog
          </NuxtLink>
          <nav v-if="state.authenticated" aria-label="Main" class="nav-tabs">
            <NuxtLink to="/" class="nav-tab" exact-active-class="is-active"
              >Posts</NuxtLink
            >
            <NuxtLink to="/settings" class="nav-tab" active-class="is-active"
              >Settings</NuxtLink
            >
          </nav>
          <UDropdownMenu
            v-if="state.authenticated"
            :items="accountItems"
            :content="{ align: 'end' }"
          >
            <UButton
              color="neutral"
              variant="ghost"
              trailing-icon="i-lucide-chevron-down"
              :aria-label="`Account: ${state.username}`"
            >
              <span class="avatar" aria-hidden="true">{{
                state.username.slice(0, 1).toUpperCase()
              }}</span>
              <span class="max-sm:hidden">{{ state.username }}</span>
            </UButton>
          </UDropdownMenu>
        </div>
      </header>
      <main id="main" tabindex="-1">
        <p v-if="error" class="alert content mt-6" role="alert">{{ error }}</p>
        <p v-if="!state.ready" class="loading" role="status">Opening BurzBlog…</p>
        <div v-else-if="!state.authenticated" class="content">
          <LoginForm />
        </div>
        <NuxtPage v-else @sign-out="requestSignOut" />
      </main>

      <UModal
        v-model:open="state.expired"
        title="Your session ended"
        description="Sign in again to keep going. Anything you have not saved is still on this page."
        :dismissible="false"
      >
        <template #body>
          <LoginForm
            compact
            :username="state.username"
            @success="state.expired = false"
          />
        </template>
      </UModal>
      <ShortcutsDialog v-if="state.authenticated" v-model:open="shortcutsOpen" />
      <ConfirmDialog
        v-model:open="confirmingSignOut"
        title="Sign out with unsaved changes?"
        body="The post you are editing has changes that are not saved. Signing out discards them."
        confirm-label="Discard and sign out"
        confirm-color="error"
        @confirm="leave"
      />
    </div>
  </UApp>
</template>
