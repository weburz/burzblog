<script setup lang="ts">
import { shortcutGroups } from "../utils/shortcuts";

const open = defineModel<boolean>("open", { default: false });
</script>

<template>
  <UModal
    v-model:open="open"
    title="Keyboard shortcuts"
    description="Shortcuts that work in BurzBlog."
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
