<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import type { GitState } from "../types";
import { ApiError, request, errorMessage } from "../utils/api";
import { withRetainedFocus } from "../utils/focus";

const toast = useToast();
const notifySuccess = (title: string) => toast.add({ title, color: "success" });
const state = ref<GitState | null>(null);
const error = ref("");
const remoteFailure = ref(false);
const lastAction = ref("");
const lastCode = ref("");
const invalidFailure = ref(false);
const busy = ref(false);
const selected = ref<string[]>([]);
const message = ref("");
const branch = ref("");
const titleElement = ref<HTMLElement | null>(null);
const remoteLabel = computed(() => {
  const remote = state.value?.remote;
  if (!remote) {
    return "";
  }
  if (remote.transport === "file") {
    return `${remote.name} · local repository`;
  }
  return `${remote.name} · HTTPS${remote.authenticated ? " with operator credentials" : ", no credentials"}`;
});
const plural = (count: number, noun: string) =>
  `${count} ${noun}${count === 1 ? "" : "s"}`;
const repository = computed(() => state.value?.repository ?? null);
const changed = computed(() => repository.value?.changed_markdown ?? []);
const heading = computed(() => {
  if (!repository.value) return "";
  if (repository.value.operation) return "Repository needs attention";
  if (changed.value.length)
    return `${plural(changed.value.length, "Markdown file")} changed`;
  return repository.value.clean ? "No local changes" : "Changes outside Markdown";
});
const ahead = computed(() => repository.value?.ahead ?? 0);
const behind = computed(() => repository.value?.behind ?? 0);
const applyPrimary = computed(
  () => behind.value > 0 && ahead.value === 0 && !!repository.value?.clean,
);
const pushPrimary = computed(() => ahead.value > 0 && behind.value === 0);
const syncNote = computed(() => {
  const upstream = repository.value?.upstream ?? "the remote";
  if (ahead.value > 0 && behind.value > 0)
    return `Both this branch and ${upstream} have new commits. Apply safe update and Push commits will both be refused until the histories are reconciled with ordinary Git.`;
  if (behind.value > 0 && repository.value?.clean)
    return `${upstream} has ${plural(behind.value, "new commit")}. Apply safe update brings them in.`;
  if (behind.value > 0)
    return `${upstream} has ${plural(behind.value, "new commit")}. Commit your Markdown changes first; a safe update needs a clean working tree.`;
  if (ahead.value > 0) return `${plural(ahead.value, "commit")} waiting to be pushed.`;
  return "";
});
const syncAction = computed(() => ["fetch", "update", "push"].includes(lastAction.value));
const errorHeading = computed(() => {
  if (remoteFailure.value) return "Remote unavailable";
  return !lastAction.value || syncAction.value
    ? "Sync needs attention"
    : "That did not work";
});
const allSelected = computed(
  () => changed.value.length > 0 && selected.value.length === changed.value.length,
);
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
    try {
      state.value = await request<GitState>("/api/git");
      selected.value = keep.filter((path) => changed.value.includes(path));
    } catch (cause) {
      error.value = errorMessage(cause);
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
      try {
        state.value = await request<GitState>("/api/git", "POST", {
          action: name,
          branch:
            name === "push" || name === "update"
              ? (state.value?.repository?.branch ?? "")
              : branch.value,
          paths: selected.value,
          message: message.value,
        });
        selected.value = [];
        const remote = state.value?.remote?.name ?? "remote";
        if (name === "commit") {
          message.value = "";
          notifySuccess(`Committed ${plural(committed, "file")}`);
        } else if (name === "push") notifySuccess(`Pushed to ${remote}`);
        else if (name === "update") notifySuccess(`Updated from ${remote}`);
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
onMounted(() => {
  titleElement.value?.focus();
  void refresh();
});
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
        :loading="busy"
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
    <p v-if="state && !state.repository" class="quiet-block">
      This content folder is not a Git repository. Writing works normally without it.
    </p>
    <template v-if="state?.repository">
      <div class="state-strip">
        <span class="chip mono"
          ><UIcon name="i-lucide-git-branch" class="size-4" />{{
            state.repository.branch ?? "Detached HEAD"
          }}</span
        >
        <span class="mono muted">{{
          state.repository.upstream ? `→ ${state.repository.upstream}` : "No upstream"
        }}</span>
        <span v-if="remoteLabel" class="mono muted">Remote: {{ remoteLabel }}</span>
        <span v-if="state.repository.ahead !== null" class="strip-counts">
          <span class="chip mono"
            >↑ {{ state.repository.ahead
            }}<span class="sr-only"
              >{{ state.repository.ahead }} commits ahead</span
            ></span
          >
          <span class="chip mono"
            >↓ {{ state.repository.behind
            }}<span class="sr-only"
              >{{ state.repository.behind }} commits behind</span
            ></span
          >
          <span class="meta">as of last check</span>
        </span>
      </div>
      <h2 class="section-title mt-6">{{ heading }}</h2>
      <p v-if="state.repository.operation" class="mt-2 text-[var(--bb-warn)]">
        Unfinished {{ state.repository.operation }}. Ask the operator to review the
        repository before further changes.
      </p>
      <p v-if="syncNote" class="muted mt-2">{{ syncNote }}</p>
      <h2 class="section-title mt-10">Sync</h2>
      <ul class="sync-list">
        <li>
          <UButton
            color="neutral"
            variant="outline"
            :disabled="busy || !state.remote_configured"
            @click="action('fetch')"
            >Check for updates</UButton
          >
          <span class="meta"
            >Contacts the remote and refreshes the counts. Changes nothing on disk.</span
          >
        </li>
        <li>
          <UButton
            :color="applyPrimary ? 'primary' : 'neutral'"
            :variant="applyPrimary ? 'solid' : 'outline'"
            :data-primary="applyPrimary ? 'true' : 'false'"
            :disabled="busy || !state.remote_configured || !state.repository.clean"
            @click="action('update')"
            >Apply safe update</UButton
          >
          <span class="meta">{{
            state.repository.clean
              ? "Fast-forwards this branch only if nothing local would be overwritten."
              : "Needs a clean working tree. Commit local changes first."
          }}</span>
        </li>
        <li>
          <UButton
            :color="pushPrimary ? 'secondary' : 'neutral'"
            :variant="pushPrimary ? 'solid' : 'outline'"
            :disabled="busy || !state.remote_configured"
            @click="action('push')"
            >Push commits</UButton
          >
          <span class="meta"
            >Sends your commits on {{ state.repository.branch ?? "this branch" }} to
            {{ state.remote?.name ?? "the remote" }}.</span
          >
        </li>
      </ul>
      <p v-if="!state.remote_configured" class="meta mt-3">
        No remote endpoint configured by the operator. Local branches and commits still
        work. Operators set <code>BURZBLOG_REMOTE_URL</code> (and, for private
        repositories, a username and token file) before starting BurzBlog.
      </p>
      <form class="mt-10 max-w-xl" @submit.prevent="action('commit')">
        <h2 class="section-title mb-2">Commit</h2>
        <div v-if="changed.length" class="file-panel">
          <div class="file-panel-head">
            <span class="meta">{{ plural(changed.length, "file") }}</span>
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
            <label v-for="path in changed" :key="path" class="file-row mono"
              ><input v-model="selected" type="checkbox" :value="path" />{{ path }}</label
            >
          </div>
        </div>
        <p v-if="!changed.length" class="muted mb-4">No changed Markdown files.</p>
        <div v-if="changed.length" class="field mt-5">
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
        <template v-if="changed.length"
          ><UButton
            type="submit"
            :color="selected.length ? 'primary' : 'neutral'"
            :variant="selected.length ? 'solid' : 'outline'"
            :disabled="busy || !selected.length"
            >Commit selected files</UButton
          >
          <p class="meta mt-2">
            Only the files ticked above are committed. Other staged changes stay as they
            are.
          </p></template
        >
      </form>
      <details class="file-options mt-10">
        <summary class="muted cursor-pointer">
          Branches and file details
          <UIcon name="i-lucide-chevron-down" class="file-options-chevron size-4" />
        </summary>
        <p class="my-4 flex flex-wrap gap-2">
          <span
            v-for="name in state.repository.branches"
            :key="name"
            class="chip mono"
            :class="{ 'chip-current': name === state.repository.branch }"
            :aria-current="name === state.repository.branch ? 'true' : undefined"
            >{{ name }}</span
          >
        </p>
        <div class="field max-w-md">
          <label for="branch">Branch name</label><UInput id="branch" v-model="branch" />
        </div>
        <div class="actions">
          <UButton
            color="neutral"
            variant="outline"
            :disabled="busy || !branch"
            @click="action('create_branch')"
            >Create branch</UButton
          ><UButton
            color="neutral"
            variant="outline"
            :disabled="busy || !branch || !state.repository.clean"
            @click="action('switch')"
            >Switch branch</UButton
          >
        </div>
        <p class="meta mt-2">Switching needs a clean working tree.</p>
        <h3 class="mt-6 font-semibold">All changes</h3>
        <p class="meta">
          Raw Git status for everything under the repository, including non-Markdown
          files.
        </p>
        <p v-if="!state.repository.changes.length" class="meta mt-2">
          Nothing has changed.
        </p>
        <div
          v-else
          class="file-panel file-list change-list mt-2"
          role="region"
          tabindex="0"
          aria-label="Raw Git status"
        >
          <ul>
            <li
              v-for="change in state.repository.changes"
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
