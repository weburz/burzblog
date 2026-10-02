<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import type { GitState } from "../types";
import { ApiError, request, errorMessage } from "../utils/api";

const state = ref<GitState | null>(null);
const error = ref("");
const remoteFailure = ref(false);
const busy = ref(false);
const selected = ref<string[]>([]);
const message = ref("");
const branch = ref("");
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
const refresh = async () => {
  try {
    state.value = await request<GitState>("/api/git");
    selected.value = [];
  } catch (cause) {
    error.value = errorMessage(cause);
  }
};
const action = async (name: string) => {
  busy.value = true;
  error.value = "";
  remoteFailure.value = false;
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
  } catch (cause) {
    error.value = errorMessage(cause);
    remoteFailure.value = cause instanceof ApiError && cause.code === "remote";
    await refresh();
  } finally {
    busy.value = false;
  }
};
onMounted(refresh);
</script>

<template>
  <section class="panel">
    <div class="toolbar">
      <h1 class="page-title">Repository</h1>
      <UButton color="neutral" variant="ghost" :disabled="busy" @click="refresh"
        >Refresh</UButton
      >
    </div>
    <p class="muted mt-4">
      Optional Git controls. Saving a post always writes its Markdown file first.
    </p>
    <div v-if="error" class="error" role="alert">
      <h2 class="font-semibold">
        {{ remoteFailure ? "Remote unavailable" : "Sync needs attention" }}
      </h2>
      <p>{{ error }}</p>
      <p v-if="!remoteFailure" class="mt-2">
        No conflict is resolved automatically. Review both histories using ordinary Git
        before retrying.
      </p>
    </div>
    <p v-if="state && !state.repository" class="my-12 muted">
      This content directory is not in a Git repository. Writing still works normally.
    </p>
    <template v-if="state?.repository">
      <h2 class="text-xl font-semibold mt-10">
        {{
          state.repository.operation
            ? "Repository needs attention"
            : state.repository.clean
              ? "No local changes"
              : "Changes not synced"
        }}
      </h2>
      <p class="font-mono text-sm muted mt-3">
        {{ state.repository.branch ?? "Detached HEAD" }} ·
        {{ state.repository.upstream ?? "No upstream configured" }}
      </p>
      <p v-if="remoteLabel" class="font-mono text-sm muted mt-2">
        Remote: {{ remoteLabel }}
      </p>
      <p v-if="state.repository.ahead !== null" class="muted mt-2">
        {{ state.repository.ahead }} ahead · {{ state.repository.behind }} behind (last
        fetched state)
      </p>
      <p v-if="state.repository.operation" class="error">
        Unfinished {{ state.repository.operation }}. Ask the operator to review the
        repository before further changes.
      </p>
      <div class="actions my-8">
        <UButton
          color="neutral"
          variant="outline"
          :disabled="busy || !state.remote_configured"
          @click="action('fetch')"
          >Check for updates</UButton
        ><UButton
          color="neutral"
          variant="outline"
          :disabled="busy || !state.remote_configured || !state.repository.clean"
          @click="action('update')"
          >Apply safe update</UButton
        ><UButton
          color="secondary"
          :disabled="busy || !state.remote_configured"
          @click="action('push')"
          >Push commits</UButton
        >
      </div>
      <p v-if="!state.remote_configured" class="muted text-sm">
        No remote endpoint configured by the operator. Local branches and commits still
        work. Operators set <code>BURZBLOG_REMOTE_URL</code> (and, for private
        repositories, a username and token file) before starting BurzBlog.
      </p>
      <form class="my-10 max-w-xl" @submit.prevent="action('commit')">
        <h2 class="text-xl font-semibold mb-4">Commit selected Markdown changes</h2>
        <label
          v-for="path in state.repository.changed_markdown"
          :key="path"
          class="flex items-center gap-3 py-2"
          ><input v-model="selected" type="checkbox" :value="path" /><span
            class="font-mono text-sm break-all"
            >{{ path }}</span
          ></label
        >
        <p v-if="!state.repository.changed_markdown.length" class="muted mb-4">
          No changed Markdown files.
        </p>
        <div class="field mt-5">
          <label for="commit-message">Commit message</label
          ><UInput id="commit-message" v-model="message" class="w-full" required />
        </div>
        <UButton
          type="submit"
          color="neutral"
          variant="outline"
          :disabled="busy || !selected.length"
          >Commit selected files</UButton
        >
      </form>
      <details>
        <summary class="muted cursor-pointer">Branches and file details</summary>
        <p class="font-mono text-sm my-4">{{ state.repository.branches.join(" · ") }}</p>
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
        <ul class="font-mono text-xs mt-6">
          <li
            v-for="change in state.repository.changes"
            :key="change.path"
            class="py-1 break-all"
          >
            {{ change.index }}{{ change.worktree }} {{ change.path }}
          </li>
        </ul>
      </details>
    </template>
  </section>
</template>
