<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from "vue";
import type { Settings, Token } from "../types";
import { errorMessage, request } from "../utils/api";
import { themeOptions } from "../utils/theme";
import { editedLabel, exactTime } from "../utils/time";

useHead({ title: "Settings" });
const toast = useToast();
const theme = usePreferredTheme();
const { enabled: shortcutsEnabled } = useEnabledShortcuts();

const settings = ref<Settings | null>(null);
const tokens = ref<Token[] | null>(null);
const error = ref("");

const load = async () => {
  try {
    const [s, t] = await Promise.all([
      request<Settings>("/api/settings"),
      request<{ items: Token[] }>("/api/tokens"),
    ]);
    settings.value = s;
    tokens.value = t.items;
  } catch (cause) {
    error.value = errorMessage(cause);
  }
};
onMounted(load);

// Account
const current = ref("");
const next = ref("");
const repeat = ref("");
const passwordBusy = ref(false);
const passwordError = ref("");
const mismatch = computed(() => repeat.value !== "" && next.value !== repeat.value);
const changePassword = async () => {
  if (mismatch.value) return;
  passwordBusy.value = true;
  passwordError.value = "";
  try {
    await request("/api/account/password", "POST", {
      current: current.value,
      next: next.value,
    });
    current.value = next.value = repeat.value = "";
    toast.add({
      title: "Password changed",
      description: "Other browsers were signed out. Terminal tokens keep working.",
      color: "success",
    });
  } catch (cause) {
    passwordError.value = errorMessage(cause);
  } finally {
    passwordBusy.value = false;
  }
};

// Tokens
const cliTokens = computed(() => tokens.value?.filter((t) => t.scope === "write") ?? []);
const buildTokens = computed(
  () => tokens.value?.filter((t) => t.scope === "export") ?? [],
);
const revoking = ref<Token | null>(null);
const revoke = async () => {
  const token = revoking.value;
  revoking.value = null;
  if (!token) return;
  try {
    await request(`/api/tokens/${token.id}`, "DELETE");
    tokens.value = tokens.value?.filter((t) => t.id !== token.id) ?? null;
    toast.add({
      title: "Access revoked",
      description: `${token.name} can no longer reach BurzBlog.`,
      color: "success",
    });
  } catch (cause) {
    toast.add({
      title: "Could not revoke",
      description: errorMessage(cause),
      color: "error",
    });
  }
};

const creatingBuild = ref(false);
const buildName = ref("Site build");
const newSecret = ref<Token | null>(null);
const newTokenButton = ref<{ $el: HTMLElement } | null>(null);
// The form replaces the button, so Cancel hands focus back to it.
const cancelBuildToken = async () => {
  creatingBuild.value = false;
  await nextTick();
  newTokenButton.value?.$el.focus();
};
const createBuildToken = async () => {
  try {
    const token = await request<Token>("/api/tokens", "POST", {
      name: buildName.value,
      scope: "export",
    });
    newSecret.value = token;
    tokens.value = [{ ...token, secret: undefined }, ...(tokens.value ?? [])];
    creatingBuild.value = false;
  } catch (cause) {
    toast.add({
      title: "Could not create the token",
      description: errorMessage(cause),
      color: "error",
    });
  }
};

// Deploys
const deploying = ref(false);
let poll: ReturnType<typeof setInterval> | undefined;
const rebuild = async () => {
  deploying.value = true;
  try {
    const status = await request<Settings["deploy"]>("/api/deploy", "POST", {});
    if (settings.value) settings.value.deploy = status;
    clearInterval(poll);
    poll = setInterval(async () => {
      const s = await request<Settings>("/api/settings").catch(() => null);
      if (s && settings.value) settings.value.deploy = s.deploy;
      if (s && !s.deploy.pending) {
        clearInterval(poll);
        deploying.value = false;
      }
    }, 1500);
  } catch (cause) {
    deploying.value = false;
    toast.add({
      title: "Could not start a rebuild",
      description: errorMessage(cause),
      color: "error",
    });
  }
};
onUnmounted(() => clearInterval(poll));

const origin = computed(
  () =>
    settings.value?.origin ??
    (typeof window === "undefined" ? "" : window.location.origin),
);
const exportCommand = computed(
  () =>
    `curl -fsSL -H "Authorization: Bearer $BURZBLOG_TOKEN" \\\n  ${origin.value}/api/export.tar.gz | tar -xz -C content/blogs`,
);
const scopeUse = (t: Token) =>
  t.lastUsedAt ? `Last used ${editedLabel(t.lastUsedAt).toLowerCase()}` : "Never used";
</script>

<template>
  <div class="content settings">
    <div class="page-head">
      <div>
        <h1 class="page-title">Settings</h1>
        <p v-if="settings" class="muted">
          Signed in as {{ settings.username }} on {{ settings.origin }}
        </p>
      </div>
    </div>
    <p v-if="error" class="alert" role="alert">{{ error }}</p>

    <section class="settings-section" aria-labelledby="terminal-heading">
      <div class="settings-aside">
        <h2 id="terminal-heading" class="section-title">Write from your terminal</h2>
        <p class="muted">
          Use your own editor. The <span class="mono">burzblog</span> command downloads a
          post, opens it in <span class="mono">$EDITOR</span> and saves it back here when
          you close the file.
        </p>
      </div>
      <div class="settings-main">
        <CopyField
          label="Commands to connect a computer"
          multiline
          :value="`burzblog login ${origin}\nburzblog list\nburzblog edit <slug>`"
        />
        <h3 class="sub-title">Connected computers</h3>
        <p v-if="tokens && !cliTokens.length" class="muted">
          None yet. Each computer that runs
          <span class="mono">burzblog login</span> appears here.
        </p>
        <ul v-else class="token-list">
          <li v-for="t in cliTokens" :key="t.id" class="token-row">
            <UIcon name="i-lucide-laptop" class="size-5 text-[var(--bb-text-2)]" />
            <div class="token-main">
              <span class="token-name">{{ t.name }}</span>
              <span class="muted"
                >{{ t.username }}, added {{ exactTime(t.createdAt) }}.
                {{ scopeUse(t) }}.</span
              >
            </div>
            <UButton color="error" variant="ghost" size="sm" @click="revoking = t"
              >Revoke</UButton
            >
          </li>
        </ul>
      </div>
    </section>

    <section class="settings-section" aria-labelledby="site-heading">
      <div class="settings-aside">
        <h2 id="site-heading" class="section-title">Your website</h2>
        <p class="muted">
          Your site downloads published posts while it builds, as Markdown files. BurzBlog
          can ask it to rebuild whenever you publish.
        </p>
      </div>
      <div class="settings-main">
        <div
          v-if="settings"
          class="deploy-card"
          :data-state="settings.deploy.lastStatus || 'none'"
        >
          <template v-if="settings.deploy.configured">
            <div>
              <p class="deploy-title">
                <UIcon
                  :name="
                    settings.deploy.lastStatus === 'failed'
                      ? 'i-lucide-circle-alert'
                      : 'i-lucide-rocket'
                  "
                  class="size-4"
                />
                <template v-if="settings.deploy.pending">Rebuild requested…</template>
                <template v-else-if="settings.deploy.lastStatus === 'ok'"
                  >Last rebuild requested
                  {{ editedLabel(settings.deploy.lastAt!).toLowerCase() }}</template
                >
                <template v-else-if="settings.deploy.lastStatus === 'failed'"
                  >The last rebuild request failed</template
                >
                <template v-else>Rebuilds run automatically after you publish</template>
              </p>
              <p v-if="settings.deploy.lastError" class="field-error">
                {{ settings.deploy.lastError }}
              </p>
            </div>
            <UButton
              color="neutral"
              variant="outline"
              icon="i-lucide-refresh-cw"
              :loading="deploying"
              @click="rebuild"
              >Rebuild now</UButton
            >
          </template>
          <p v-else class="muted">
            Automatic rebuilds are off. Start BurzBlog with
            <span class="mono">BURZBLOG_DEPLOY_HOOK</span> set to your host's deploy hook
            URL, such as a Vercel deploy hook.
          </p>
        </div>

        <h3 class="sub-title">Build tokens</h3>
        <p class="muted">
          A build token can only download published posts. Store it as a secret in your
          host.
        </p>

        <div v-if="newSecret" class="secret-reveal" role="alert">
          <p><strong>Copy this token now.</strong> It is shown only once.</p>
          <CopyField label="New build token" :value="newSecret.secret ?? ''" />
          <p class="field-hint">Then fetch posts in your build step:</p>
          <CopyField label="Build command" multiline :value="exportCommand" />
          <UButton color="neutral" variant="ghost" size="sm" @click="newSecret = null"
            >I have saved it</UButton
          >
        </div>

        <form v-if="creatingBuild" class="inline-form" @submit.prevent="createBuildToken">
          <div class="field flex-1">
            <label for="build-name">Name</label>
            <UInput
              id="build-name"
              v-model="buildName"
              required
              maxlength="100"
              autofocus
              class="w-full"
            />
          </div>
          <UButton type="submit" color="secondary">Create token</UButton>
          <UButton color="neutral" variant="ghost" @click="cancelBuildToken"
            >Cancel</UButton
          >
        </form>
        <UButton
          v-else-if="!newSecret"
          ref="newTokenButton"
          color="neutral"
          variant="outline"
          icon="i-lucide-key-round"
          @click="creatingBuild = true"
          >New build token</UButton
        >

        <ul v-if="buildTokens.length" class="token-list">
          <li v-for="t in buildTokens" :key="t.id" class="token-row">
            <UIcon name="i-lucide-package" class="size-5 text-[var(--bb-text-2)]" />
            <div class="token-main">
              <span class="token-name">{{ t.name }}</span>
              <span class="muted"
                >Added {{ exactTime(t.createdAt) }}. {{ scopeUse(t) }}.</span
              >
            </div>
            <UButton color="error" variant="ghost" size="sm" @click="revoking = t"
              >Revoke</UButton
            >
          </li>
        </ul>
      </div>
    </section>

    <section class="settings-section" aria-labelledby="backup-heading">
      <div class="settings-aside">
        <h2 id="backup-heading" class="section-title">Backup</h2>
        <p class="muted">
          One download with every post, including drafts and archived ones, as Markdown
          files.
        </p>
      </div>
      <div class="settings-main">
        <div>
          <UButton
            as="a"
            href="/api/backup.tar.gz"
            external
            download
            color="neutral"
            variant="outline"
            icon="i-lucide-download"
            >Download all posts</UButton
          >
        </div>
        <p class="field-hint">
          History is not included. The database file is the complete backup, with every
          earlier version; see
          <a
            href="https://github.com/weburz/burzblog/blob/main/docs/self-hosting.md#backups"
            target="_blank"
            rel="noopener noreferrer"
            >Backups in the self-hosting guide</a
          >.
        </p>
      </div>
    </section>

    <section class="settings-section" aria-labelledby="appearance-heading">
      <div class="settings-aside">
        <h2 id="appearance-heading" class="section-title">Appearance</h2>
        <p class="muted">
          Saved in this browser. System follows your computer's setting. Turn off keyboard
          shortcuts if single keys get in your way.
        </p>
      </div>
      <div class="settings-main">
        <div class="segment" role="radiogroup" aria-labelledby="appearance-heading">
          <button
            v-for="option in themeOptions"
            :key="option.value"
            type="button"
            role="radio"
            class="segment-item"
            :aria-checked="theme.preference.value === option.value"
            @click="theme.preference.value = option.value"
          >
            <UIcon :name="option.icon" class="size-4" aria-hidden="true" />
            {{ option.label }}
          </button>
        </div>
        <USwitch
          v-model="shortcutsEnabled"
          label="Keyboard shortcuts"
          description="Single keys on the posts list (n and /) and ? for the shortcut list. Shortcuts with Ctrl or Cmd always work."
        />
      </div>
    </section>

    <section class="settings-section" aria-labelledby="account-heading">
      <div class="settings-aside">
        <h2 id="account-heading" class="section-title">Password</h2>
        <p class="muted">
          Changing it signs out every other browser. Terminal and build tokens keep
          working.
        </p>
      </div>
      <form class="settings-main password-form" @submit.prevent="changePassword">
        <input
          type="text"
          autocomplete="username"
          :value="settings?.username"
          hidden
          readonly
        />
        <div class="field">
          <label for="pw-current">Current password</label>
          <UInput
            id="pw-current"
            v-model="current"
            type="password"
            autocomplete="current-password"
            required
            class="w-full"
          />
        </div>
        <div class="field">
          <label for="pw-next">New password</label>
          <UInput
            id="pw-next"
            v-model="next"
            type="password"
            autocomplete="new-password"
            required
            minlength="12"
            class="w-full"
            aria-describedby="pw-next-hint"
          />
          <p id="pw-next-hint" class="field-hint">
            At least 12 characters. A few unrelated words work well.
          </p>
        </div>
        <div class="field">
          <label for="pw-repeat">Repeat new password</label>
          <UInput
            id="pw-repeat"
            v-model="repeat"
            type="password"
            autocomplete="new-password"
            required
            class="w-full"
            :color="mismatch ? 'error' : undefined"
            :aria-describedby="mismatch ? 'pw-repeat-error' : undefined"
          />
          <p v-if="mismatch" id="pw-repeat-error" class="field-error">
            The two new passwords are different.
          </p>
        </div>
        <p v-if="passwordError" class="alert" role="alert">{{ passwordError }}</p>
        <div>
          <UButton
            type="submit"
            color="secondary"
            :loading="passwordBusy"
            :disabled="mismatch"
            >Change password</UButton
          >
        </div>
      </form>
    </section>

    <p v-if="settings" class="list-foot muted">BurzBlog {{ settings.version }}</p>

    <ConfirmDialog
      :open="revoking !== null"
      :title="`Revoke ${revoking?.name ?? 'this token'}?`"
      :body="
        revoking?.scope === 'export'
          ? 'Builds using this token will fail until you give them a new one.'
          : 'That computer will need to run burzblog login again.'
      "
      confirm-label="Revoke"
      confirm-color="error"
      @update:open="(v: boolean) => !v && (revoking = null)"
      @confirm="revoke"
    />
  </div>
</template>
