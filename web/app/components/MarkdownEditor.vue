<script setup lang="ts">
import { ref } from "vue";
import { codeBlock, link, prefixLines, wrap, type Edit } from "../utils/markdown";

const model = defineModel<string>({ required: true });
defineProps<{ readonly?: boolean }>();
const area = ref<HTMLTextAreaElement | null>(null);

// Apply an edit through insertText so the browser's undo history keeps it.
const apply = (edit: Edit) => {
  const el = area.value;
  if (!el) return;
  const old = el.value;
  let head = 0;
  while (head < old.length && head < edit.text.length && old[head] === edit.text[head])
    head++;
  let tail = 0;
  while (
    tail < old.length - head &&
    tail < edit.text.length - head &&
    old[old.length - 1 - tail] === edit.text[edit.text.length - 1 - tail]
  )
    tail++;
  el.focus();
  el.setSelectionRange(head, old.length - tail);
  const inserted = edit.text.slice(head, edit.text.length - tail);
  const ok = inserted
    ? document.execCommand("insertText", false, inserted)
    : document.execCommand("delete");
  if (!ok || el.value !== edit.text) {
    el.value = edit.text;
  }
  model.value = el.value;
  el.setSelectionRange(edit.start, edit.end);
};

const run = (kind: string) => {
  const el = area.value;
  if (!el) return;
  const { value, selectionStart: s, selectionEnd: e } = el;
  const edits: Record<string, () => Edit> = {
    bold: () => wrap(value, s, e, "**"),
    italic: () => wrap(value, s, e, "_"),
    code: () => wrap(value, s, e, "`"),
    h2: () => prefixLines(value, s, e, "## "),
    h3: () => prefixLines(value, s, e, "### "),
    quote: () => prefixLines(value, s, e, "> "),
    bullets: () => prefixLines(value, s, e, "- "),
    numbers: () => prefixLines(value, s, e, "1. "),
    link: () => link(value, s, e),
    block: () => codeBlock(value, s, e),
  };
  apply(edits[kind]!());
};

const keydown = (event: KeyboardEvent) => {
  if (!(event.metaKey || event.ctrlKey) || event.altKey) return;
  const key = event.key.toLowerCase();
  const map: Record<string, string> = { b: "bold", i: "italic", k: "link", e: "code" };
  if (map[key] && !event.shiftKey) {
    event.preventDefault();
    run(map[key]);
  }
};

// Pasting a link over selected words turns them into a Markdown link.
const paste = (event: ClipboardEvent) => {
  const el = area.value;
  const text = event.clipboardData?.getData("text/plain")?.trim() ?? "";
  if (!el || el.selectionStart === el.selectionEnd || !/^https?:\/\/\S+$/.test(text))
    return;
  event.preventDefault();
  const { value, selectionStart: s, selectionEnd: e } = el;
  const label = value.slice(s, e);
  const inserted = `[${label}](${text})`;
  apply({
    text: value.slice(0, s) + inserted + value.slice(e),
    start: s + inserted.length,
    end: s + inserted.length,
  });
};

const tools = [
  { kind: "h2", icon: "i-lucide-heading-2", label: "Heading" },
  { kind: "h3", icon: "i-lucide-heading-3", label: "Subheading" },
  { kind: "bold", icon: "i-lucide-bold", label: "Bold", keys: ["meta", "b"] },
  { kind: "italic", icon: "i-lucide-italic", label: "Italic", keys: ["meta", "i"] },
  { kind: "link", icon: "i-lucide-link", label: "Link", keys: ["meta", "k"] },
  { kind: "quote", icon: "i-lucide-text-quote", label: "Quote" },
  { kind: "bullets", icon: "i-lucide-list", label: "Bulleted list" },
  { kind: "numbers", icon: "i-lucide-list-ordered", label: "Numbered list" },
  { kind: "code", icon: "i-lucide-code", label: "Inline code", keys: ["meta", "e"] },
  { kind: "block", icon: "i-lucide-square-code", label: "Code block" },
];

defineExpose({ focus: () => area.value?.focus(), element: area });
</script>

<template>
  <div class="md-editor">
    <div v-if="!readonly" class="md-tools" role="toolbar" aria-label="Formatting">
      <UTooltip
        v-for="tool in tools"
        :key="tool.kind"
        :text="tool.label"
        :kbds="tool.keys"
      >
        <UButton
          :icon="tool.icon"
          color="neutral"
          variant="ghost"
          size="sm"
          :aria-label="tool.label"
          @mousedown.prevent
          @click="run(tool.kind)"
        />
      </UTooltip>
    </div>
    <textarea
      ref="area"
      v-model="model"
      class="md-source"
      aria-label="Post body in Markdown"
      placeholder="Write in Markdown. # for headings, **bold**, [links](https://…)"
      spellcheck="true"
      :readonly="readonly"
      @keydown="keydown"
      @paste="paste"
    />
  </div>
</template>
