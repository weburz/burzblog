<script setup lang="ts">
import { onMounted, ref } from "vue";
import type { AuditEntry, AuditList } from "../types";
import { errorMessage, request } from "../utils/api";
import { auditActor, auditLabel, auditNeedsAttention } from "../utils/audit";
import { exactTime } from "../utils/time";

useHead({ title: "Audit log" });
const { state, isAdmin } = useCurrentSession();

const PAGE = 50;
const entries = ref<AuditEntry[]>([]);
const total = ref(0);
const loading = ref(false);
const error = ref("");

const load = async () => {
  loading.value = true;
  error.value = "";
  try {
    const page = await request<AuditList>(
      `/api/audit?limit=${PAGE}&offset=${entries.value.length}`,
    );
    entries.value = [...entries.value, ...page.items];
    total.value = page.total;
  } catch (cause) {
    error.value = errorMessage(cause);
  } finally {
    loading.value = false;
  }
};
// Authors have no audit log; the server would refuse them anyway.
onMounted(() => {
  if (isAdmin.value) void load();
});
</script>

<template>
  <div class="content settings">
    <div class="page-head">
      <div>
        <h1 class="page-title">Audit log</h1>
        <p class="muted">
          Sign-ins, tokens, passwords, publishing, deleting and rebuilds, newest first.
          Entries cannot be changed or removed.
        </p>
      </div>
    </div>

    <p v-if="!isAdmin" class="alert" role="alert">
      Only admins can read the audit log. You are signed in as
      {{ state.username }}, an {{ state.role }}.
    </p>
    <template v-else>
      <p v-if="error" class="alert" role="alert">{{ error }}</p>
      <p v-if="!loading && !error && !entries.length" class="muted">
        Nothing has been recorded yet.
      </p>
      <div v-if="entries.length" class="audit-wrap">
        <table class="audit-table">
          <caption class="sr-only">
            Audit log, newest first
          </caption>
          <thead>
            <tr>
              <th scope="col">When</th>
              <th scope="col">Who</th>
              <th scope="col">What</th>
              <th scope="col">Target</th>
              <th scope="col">From</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="entry in entries"
              :key="entry.id"
              :data-attention="auditNeedsAttention(entry) || undefined"
            >
              <td>
                <time :datetime="entry.at">{{ exactTime(entry.at) }}</time>
              </td>
              <td>{{ auditActor(entry) }}</td>
              <td>{{ auditLabel(entry.action) }}</td>
              <td v-if="entry.target" class="mono" data-label="Target">
                {{ entry.target }}
              </td>
              <td v-else class="mono" />
              <td v-if="entry.remote" class="mono" data-label="From">
                {{ entry.remote }}
              </td>
              <td v-else class="mono" />
            </tr>
          </tbody>
        </table>
      </div>
      <p v-if="entries.length" class="list-foot muted">
        Showing {{ entries.length }} of {{ total }}.
      </p>
      <div v-if="entries.length < total">
        <UButton color="neutral" variant="outline" :loading="loading" @click="load"
          >Show older entries</UButton
        >
      </div>
    </template>
  </div>
</template>

<style scoped>
.audit-wrap {
  overflow-x: auto;
}
.audit-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.875rem;
}
.audit-table th,
.audit-table td {
  padding: 0.5rem 0.75rem;
  text-align: left;
  vertical-align: top;
  border-bottom: 1px solid var(--bb-border);
  white-space: nowrap;
}
.audit-table td.mono {
  white-space: normal;
  overflow-wrap: anywhere;
}
.audit-table tr[data-attention] td:nth-child(3) {
  font-weight: 600;
}

/* A phone cannot fit five columns: stack each entry instead of hiding the
   target and address off to the side. */
@media (max-width: 640px) {
  .audit-table thead {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }
  .audit-table tr {
    display: block;
    padding: 0.5rem 0;
    border-bottom: 1px solid var(--bb-border);
  }
  .audit-table td {
    display: block;
    padding: 0.125rem 0.75rem;
    border-bottom: 0;
  }
  .audit-table td:empty {
    display: none;
  }
  .audit-table td[data-label]::before {
    content: attr(data-label) ": ";
    font-family: inherit;
    color: var(--bb-text-3);
  }
}
</style>
