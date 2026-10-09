<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import type { GitState } from "../types";
import { ApiError, request, errorMessage } from "../utils/api";
import { withRetainedFocus } from "../utils/focus";

const toast = useToast();
const notifySuccess = (title: string) => toast.add({ title, color: "success" });
const state = ref<GitState | null>(null);
const loading = ref(false);
const error = ref("");
const remoteFailure = ref(false);
const lastAction = ref("");
const lastCode = ref("");
const invalidFailure = ref(false);
const busy = ref(false);
const selected = ref<string[]>([]);
const message = ref("");
const newBranch = ref("");
const switchTarget = ref<string | undefined>(undefined);
const titleElement = ref<HTMLElement | null>(null);
const plural = (count: number, noun: string) =>
  `${count} ${noun}${count === 1 ? "" : "s"}`;
const repository = computed(() => state.value?.repository ?? null);
const remote = computed(() => state.value?.remote ?? null);
const remoteLabel = computed(() => {
  const value = remote.value;
  if (!value) return "";
  if (value.transport === "file") return `${value.name} · local repository`;
  return `${value.name} · HTTPS${value.authenticated ? " with operator credentials" : ", no credentials"}`;
});
const changed = computed(() => repository.value?.changed_markdown ?? []);
const unsupported = computed(() => repository.value?.unsupported_markdown ?? []);
// Changes Git sees that BurzBlog cannot commit: other files and unsupported names.
const outside = computed(() => {
  const changes = repository.value?.changes.length ?? 0;
  return Math.max(changes - changed.value.length, 0);
});
const heading = computed(() => {
  if (!repository.value) return "";
  if (repository.value.operation) return "Repository needs attention";
  if (changed.value.length)
    return `${plural(changed.value.length, "Markdown file")} changed`;
  return repository.value.clean ? "No local changes" : "Changes outside Markdown";
});
// Counts are null when there is nothing to compare with yet.
const ahead = computed(() => repository.value?.ahead ?? null);
const behind = computed(() => repository.value?.behind ?? null);
const diverged = computed(() => (ahead.value ?? 0) > 0 && (behind.value ?? 0) > 0);
const clean = computed(() => !!repository.value?.clean);
const upstreamName = computed(() => repository.value?.upstream ?? "the remote");
const branchName = computed(() => repository.value?.branch ?? "this branch");
const blockedByOutside = computed(() => !clean.value && !changed.value.length);
const canUpdate = computed(
  () =>
    !!state.value?.remote_configured &&
    clean.value &&
    !diverged.value &&
    behind.value !== 0,
);
const canPush = computed(
  () =>
    !!state.value?.remote_configured &&
    !repository.value?.detached &&
    !diverged.value &&
    ahead.value !== 0,
);
const applyPrimary = computed(() => canUpdate.value && (behind.value ?? 0) > 0);
const pushPrimary = computed(() => canPush.value && (ahead.value ?? 0) > 0);
const syncNote = computed(() => {
  if (diverged.value)
    return `Both this branch and ${upstreamName.value} have new commits. Apply safe update and Push commits will both be refused until the histories are reconciled with ordinary Git.`;
  if ((behind.value ?? 0) > 0 && clean.value)
    return `${upstreamName.value} has ${plural(behind.value!, "new commit")}. Apply safe update brings them in.`;
  if ((behind.value ?? 0) > 0 && blockedByOutside.value)
    return `${upstreamName.value} has ${plural(behind.value!, "new commit")}, but changes outside Markdown block a safe update. They need ordinary Git; see All changes.`;
  if ((behind.value ?? 0) > 0)
    return `${upstreamName.value} has ${plural(behind.value!, "new commit")}. Commit your Markdown changes first; a safe update needs a clean working tree.`;
  if ((ahead.value ?? 0) > 0)
    return `${plural(ahead.value!, "commit")} waiting to be pushed.`;
  return "";
});
const updateNote = computed(() => {
  if (blockedByOutside.value)
    return "Blocked by changes outside Markdown; see All changes.";
  if (!clean.value) return "Needs a clean working tree. Commit local changes first.";
  if (diverged.value) return "Refused while both sides have new commits.";
  if (behind.value === 0) return "Nothing to apply as of the last check.";
  return "Fast-forwards this branch only if nothing local would be overwritten.";
});
const pushNote = computed(() => {
  if (repository.value?.detached) return "Choose a branch before pushing.";
  if (diverged.value)
    return `Refused while ${upstreamName.value} has commits this branch lacks.`;
  if (ahead.value === 0) return "Nothing to push as of the last check.";
  const count = ahead.value ? `${plural(ahead.value, "commit")} on` : "Your commits on";
  return `Sends ${count} ${branchName.value} to ${remote.value?.name ?? "the remote"}.`;
});
const syncAction = computed(() => ["fetch", "update", "push"].includes(lastAction.value));
const errorHeading = computed(() => {
  if (remoteFailure.value) return "Remote unavailable";
  return !lastAction.value || syncAction.value
    ? "Sync needs attention"
    : "That did not work";
});
const otherBranches = computed(() =>
  (repository.value?.branches ?? []).filter((name) => name !== repository.value?.branch),
);
const allSelected = computed(
  () => changed.value.length > 0 && selected.value.length === changed.value.length,
);
// Short change labels from Git's two status letters, for the file list.
const kind = (path: string) => {
  const change = repository.value?.changes.find(
    (entry) => entry.path === path || entry.path.endsWith(`/${path}`),
  );
  const code = change ? `${change.index}${change.worktree}` : "";
  if (code.includes("?") || code.includes("A")) return "New";
  if (code.includes("D")) return "Deleted";
  if (code.includes("R")) return "Renamed";
  return "Modified";
};
const toggleAll = () => {
  selected.value = allSelected.value ? [] : [...changed.value];
};
const refresh = (keep: string[] = [], track = true) =>
  withRetainedFocus(async () => {
    if (track) {
      lastAction.value = "refresh";
      remoteFailure.value = false;
      invalidFailure.value = false;
      error.value = "";
    }
    loading.value = true;
    try {
      state.value = await request<GitState>("/api/git");
      selected.value = keep.filter((path) => changed.value.includes(path));
      if (switchTarget.value && !otherBranches.value.includes(switchTarget.value))
        switchTarget.value = undefined;
    } catch (cause) {
      error.value = errorMessage(cause);
      lastCode.value = cause instanceof ApiError ? cause.code : "";
    } finally {
      loading.value = false;
    }
  });
const action = (name: string) =>
  withRetainedFocus(
    async () => {
      busy.value = true;
      error.value = "";
      remoteFailure.value = false;
      invalidFailure.value = false;
      lastAction.value = name;
      lastCode.value = "";
      const committed = selected.value.length;
      const pushed = ahead.value;
      const current = repository.value?.branch ?? "";
      const branch =
        name === "push" || name === "update"
          ? current
          : name === "switch"
            ? (switchTarget.value ?? "")
            : newBranch.value.trim();
      try {
        state.value = await request<GitState>("/api/git", "POST", {
          action: name,
          branch,
          paths: selected.value,
          message: message.value,
        });
        selected.value = [];
        const remoteName = state.value?.remote?.name ?? "remote";
        if (name === "commit") {
          message.value = "";
          notifySuccess(`Committed ${plural(committed, "file")}`);
        } else if (name === "push")
          notifySuccess(
            pushed
              ? `Pushed ${plural(pushed, "commit")} to ${remoteName}`
              : `Pushed to ${remoteName}`,
          );
        else if (name === "update") notifySuccess(`Updated from ${remoteName}`);
        else if (name === "create_branch") {
          notifySuccess(`Created branch ${branch}`);
          newBranch.value = "";
        } else if (name === "switch") {
          notifySuccess(`Switched to ${branch}`);
          switchTarget.value = undefined;
        }
      } catch (cause) {
        error.value = errorMessage(cause);
        lastCode.value = cause instanceof ApiError ? cause.code : "";
        remoteFailure.value = cause instanceof ApiError && cause.code === "remote";
        invalidFailure.value =
          name === "commit" && cause instanceof ApiError && cause.code === "invalid";
        await refresh(selected.value, false);
      } finally {
        busy.value = false;
      }
    },
    () => [titleElement.value],
  );
// After signing in again, replace a stale session failure with fresh state.
const restored = () => {
  if (error.value) void refresh();
};
onMounted(() => {
  titleElement.value?.focus();
  void refresh();
  window.addEventListener("burzblog:session-restored", restored);
});
onUnmounted(() => window.removeEventListener("burzblog:session-restored", restored));
</script>

<template>
  <section class="panel">
    <div class="toolbar">
      <h1 ref="titleElement" tabindex="-1" class="page-title">Repository</h1>
      <UButton
        color="neutral"
        variant="ghost"
        icon="i-lucide-refresh-cw"
        aria-label="Refresh"
        :loading="loading || busy"
        @click="refresh()"
      />
    </div>
    <p class="muted mt-2">
      Git controls for the content folder. Saving a post writes its file; committing and
      pushing are separate, explicit steps.
    </p>
    <div v-if="error" class="alert" role="alert">
      <h2 class="alert-title">
        {{ errorHeading }}
      </h2>
      <p>{{ error }}</p>
      <p v-if="invalidFailure" class="mt-2">
        Files that Posts marks “Needs attention” cannot be committed until they are fixed
        on disk.
      </p>
      <p
        v-if="
          !remoteFailure &&
          (lastAction === 'update' || lastAction === 'push') &&
          lastCode === 'conflict'
        "
        class="mt-2"
      >
        Nothing was merged automatically. Compare both histories with ordinary Git before
        retrying.
      </p>
      <p
        v-if="
          !remoteFailure &&
          syncAction &&
          !['conflict', 'authentication', 'network'].includes(lastCode)
        "
        class="mt-2"
      >
        Nothing local was changed. If this keeps happening, ask the operator to check the
        remote configuration.
      </p>
    </div>
    <div v-if="!state && !error" class="repo-grid mt-8" aria-busy="true">
      <p class="sr-only" role="status">Loading repository…</p>
      <div>
        <USkeleton class="h-6 w-1/2" />
        <USkeleton class="mt-4 h-32 w-full" />
      </div>
      <USkeleton class="h-48 w-full" />
    </div>
    <p v-else-if="state && !state.repository" class="quiet-block">
      This content folder is not a Git repository. Writing works normally without it.
    </p>
    <template v-if="repository">
      <div class="repo-grid mt-8">
        <div class="repo-main">
          <h2 class="section-title">{{ heading }}</h2>
          <p v-if="repository.operation" class="mt-2 text-[var(--bb-warn)]">
            Unfinished {{ repository.operation }}. Ask the operator to review the
            repository before further changes.
          </p>
          <p v-if="outside" class="muted mt-2">
            {{ plural(outside, "other change") }} BurzBlog cannot commit (files outside
            Markdown or with unsupported names); they are listed under All changes and
            need ordinary Git.
          </p>
          <form v-if="changed.length" class="mt-4" @submit.prevent="action('commit')">
            <div class="file-panel">
              <div class="file-panel-head">
                <span class="meta"
                  >{{ selected.length }} of
                  {{ plural(changed.length, "file") }} selected</span
                >
                <div class="actions">
                  <UButton
                    type="button"
                    color="neutral"
                    variant="ghost"
                    size="sm"
                    :disabled="allSelected"
                    @click="toggleAll"
                    >Select all</UButton
                  >
                  <UButton
                    type="button"
                    color="neutral"
                    variant="ghost"
                    size="sm"
                    :disabled="!selected.length"
                    @click="selected = []"
                    >Clear</UButton
                  >
                </div>
              </div>
              <div class="file-list">
                <div v-for="path in changed" :key="path" class="file-row">
                  <label class="file-label mono"
                    ><input v-model="selected" type="checkbox" :value="path" />{{
                      path
                    }}</label
                  >
                  <span class="change-kind" :data-kind="kind(path)">{{
                    kind(path)
                  }}</span>
                </div>
              </div>
            </div>
            <p v-if="unsupported.length" class="meta mt-2">
              Not offered: {{ unsupported.join(", ") }}. BurzBlog only commits filenames
              made of letters, numbers, dots, hyphens and underscores.
            </p>
            <div class="field mt-5">
              <label for="commit-message">Commit message</label
              ><UInput
                id="commit-message"
                v-model="message"
                class="w-full"
                maxlength="4000"
                placeholder="Describe the change"
                required
              />
            </div>
            <UButton
              type="submit"
              :color="selected.length ? 'primary' : 'neutral'"
              :variant="selected.length ? 'solid' : 'outline'"
              :disabled="busy || !selected.length"
              >Commit selected files</UButton
            >
            <p class="meta mt-2">
              Only the files ticked above are committed. Other staged changes stay as they
              are.
            </p>
          </form>
          <p v-else-if="repository.clean" class="muted mt-2">
            Everything in the content folder is committed.
          </p>
        </div>
        <div class="repo-side">
          <section class="side-card" aria-labelledby="sync-heading">
            <h2 id="sync-heading" class="section-title">Sync</h2>
            <div class="sync-state">
              <span class="chip mono"
                ><UIcon name="i-lucide-git-branch" class="size-4" />{{
                  repository.branch ?? "Detached HEAD"
                }}</span
              >
              <span class="mono muted">{{
                repository.upstream ? `→ ${repository.upstream}` : "No upstream"
              }}</span>
              <span v-if="ahead !== null" class="strip-counts">
                <span class="chip mono"
                  >↑ {{ ahead }}<span class="sr-only"> commits ahead</span></span
                >
                <span class="chip mono"
                  >↓ {{ behind }}<span class="sr-only"> commits behind</span></span
                >
                <span class="meta">as of last check</span>
              </span>
            </div>
            <p v-if="remoteLabel" class="mono muted text-[13px]">
              Remote: {{ remoteLabel }}
            </p>
            <p v-if="syncNote" class="sync-note">{{ syncNote }}</p>
            <template v-if="state?.remote_configured">
              <div class="sync-action">
                <UButton
                  block
                  color="neutral"
                  variant="outline"
                  :disabled="busy"
                  @click="action('fetch')"
                  >Check for updates</UButton
                >
                <p class="meta">
                  Contacts the remote and refreshes the counts. Changes nothing on disk.
                </p>
              </div>
              <div class="sync-action">
                <UButton
                  block
                  :color="applyPrimary ? 'primary' : 'neutral'"
                  :variant="applyPrimary ? 'solid' : 'outline'"
                  :data-primary="applyPrimary ? 'true' : 'false'"
                  :disabled="busy || !canUpdate"
                  @click="action('update')"
                  >Apply safe update</UButton
                >
                <p class="meta">{{ updateNote }}</p>
              </div>
              <div class="sync-action">
                <UButton
                  block
                  :color="pushPrimary ? 'secondary' : 'neutral'"
                  :variant="pushPrimary ? 'solid' : 'outline'"
                  :disabled="busy || !canPush"
                  @click="action('push')"
                  >Push commits</UButton
                >
                <p class="meta">{{ pushNote }}</p>
              </div>
            </template>
            <p v-else class="meta">
              No remote endpoint configured by the operator, so there is nothing to sync.
              Local branches and commits still work. Operators set
              <code>BURZBLOG_REMOTE_URL</code> (and, for private repositories, a username
              and token file) before starting BurzBlog.
            </p>
          </section>
          <section class="side-card" aria-labelledby="branches-heading">
            <h2 id="branches-heading" class="section-title">Branches</h2>
            <div v-if="otherBranches.length" class="branch-row">
              <label for="switch-branch" class="sr-only">Branch to switch to</label>
              <USelect
                id="switch-branch"
                v-model="switchTarget"
                :items="otherBranches"
                placeholder="Switch to…"
                class="min-w-0 flex-1"
              />
              <UButton
                color="neutral"
                variant="outline"
                :disabled="busy || !switchTarget || !repository.clean"
                @click="action('switch')"
                >Switch</UButton
              >
            </div>
            <p v-if="otherBranches.length" class="meta">
              {{
                repository.clean
                  ? "Switching rewrites the content folder to that branch."
                  : "Switching needs a clean working tree."
              }}
            </p>
            <p v-else class="meta">
              {{ repository.branch ?? "HEAD" }} is the only branch.
            </p>
            <form class="branch-row mt-3" @submit.prevent="action('create_branch')">
              <label for="new-branch" class="sr-only">New branch name</label>
              <UInput
                id="new-branch"
                v-model="newBranch"
                placeholder="New branch name"
                class="min-w-0 flex-1"
              />
              <UButton
                type="submit"
                color="neutral"
                variant="outline"
                :disabled="busy || !newBranch.trim()"
                >Create</UButton
              >
            </form>
            <p class="meta">
              Creates the branch at the current commit without switching.
            </p>
          </section>
        </div>
      </div>
      <details class="file-options mt-10">
        <summary class="muted cursor-pointer">
          All changes
          <UIcon name="i-lucide-chevron-down" class="file-options-chevron size-4" />
        </summary>
        <p class="meta mt-3">
          Raw Git status for everything under the repository, including non-Markdown
          files.
        </p>
        <p v-if="!repository.changes.length" class="meta mt-2">Nothing has changed.</p>
        <div
          v-else
          class="file-panel file-list change-list mt-2"
          role="region"
          tabindex="0"
          aria-label="Raw Git status"
        >
          <ul>
            <li
              v-for="change in repository.changes"
              :key="change.path"
              class="change-row"
            >
              {{ change.index }}{{ change.worktree }} {{ change.path }}
            </li>
          </ul>
        </div>
      </details>
    </template>
  </section>
</template>
