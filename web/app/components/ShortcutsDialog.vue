<script setup lang="ts">
import { watch } from "vue";
import { returnFocusTo } from "../utils/focus";
import { shortcutGroups } from "../utils/shortcuts";

const open = defineModel<boolean>("open", { default: false });

// Focus goes back to what had it when the dialog opened; when that is gone (a
// menu item) or was the page itself (the "?" key), to the main region.
let opener: HTMLElement | null = null;
watch(
  open,
  (value) => {
    if (value) opener = document.activeElement as HTMLElement | null;
  },
  { flush: "sync" },
);
const closeFocus = returnFocusTo(() =>
  opener?.isConnected && opener !== document.body
    ? opener
    : document.getElementById("main"),
);
</script>

<template>
  <UModal
    v-model:open="open"
    title="Keyboard shortcuts"
    description="Shortcuts that work in BurzBlog."
    :content="{ onCloseAutoFocus: closeFocus }"
  >
    <template #body>
      <section
        v-for="group in shortcutGroups"
        :key="group.title"
        class="shortcut-group"
        :aria-label="group.title"
      >
        <h3 class="shortcut-title">{{ group.title }}</h3>
        <dl class="shortcut-list">
          <div v-for="item in group.items" :key="item.label" class="shortcut-row">
            <dt>{{ item.label }}</dt>
            <dd>
              <UKbd v-for="key in item.keys" :key="key" :value="key" />
            </dd>
          </div>
        </dl>
      </section>
    </template>
  </UModal>
</template>
