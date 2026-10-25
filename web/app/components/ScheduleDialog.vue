<script setup lang="ts">
import { computed, ref, watch } from "vue";
import {
  fromLocalInput,
  scheduleProblem,
  suggestedSchedule,
  toLocalInput,
  zoneLabel,
} from "../utils/time";

const props = defineProps<{
  // The current publish time when rescheduling.
  current: string | null;
  busy: boolean;
  closeFocus?: (event: Event) => void;
}>();
const open = defineModel<boolean>("open", { default: false });
const emit = defineEmits<{ confirm: [at: string] }>();

const value = ref("");
const touched = ref(false);
// The dialog starts from the current time, or tomorrow morning, each time it opens.
watch(open, (isOpen) => {
  if (!isOpen) return;
  value.value = props.current ? toLocalInput(props.current) : suggestedSchedule();
  touched.value = false;
});

const problem = computed(() => scheduleProblem(value.value));
const minimum = computed(() =>
  open.value ? toLocalInput(new Date().toISOString()) : "",
);
const submit = () => {
  touched.value = true;
  const at = fromLocalInput(value.value);
  if (!problem.value && at) emit("confirm", at);
};
</script>

<template>
  <UModal
    v-model:open="open"
    title="Schedule this post"
    description="The post stays a draft until then, and publishes itself."
    :close="false"
    :content="{ onCloseAutoFocus: closeFocus }"
  >
    <template #body>
      <UFormField
        label="Publish at"
        :hint="zoneLabel()"
        :error="touched && problem ? problem : undefined"
      >
        <UInput
          id="schedule-at"
          v-model="value"
          type="datetime-local"
          :min="minimum"
          class="w-full"
          autofocus
          @keydown.enter.prevent="submit"
        />
      </UFormField>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" @click="open = false">Cancel</UButton>
        <UButton
          :loading="busy"
          :disabled="touched && Boolean(problem)"
          @click="submit"
          >{{ current ? "Reschedule" : "Schedule" }}</UButton
        >
      </div>
    </template>
  </UModal>
</template>
