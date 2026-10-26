<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import type { Post, PostDraft } from "../../types";
import { ApiError, errorMessage, request } from "../../utils/api";
import { returnFocusTo } from "../../utils/focus";
import { countWords, lengthLabel } from "../../utils/markdown";
import { sameDraft } from "../../utils/metadata";
import { diffTooLong, lineDiff } from "../../utils/diff";
import type { PaletteCommand } from "../../utils/palette";
import {
  editedLabel,
  exactTime,
  isScheduled,
  publishesLabel,
  scheduledLabel,
  zoneLabel,
} from "../../utils/time";

const route = useRoute();
const toast = useToast();
const dirtyFlag = useState("editor-dirty", () => false);
// A post that fails to open falls back to the normal page chrome.
const editorFailed = useState("editor-failed", () => false);
const { dark: themeDark } = usePreferredTheme();
const { register } = useRegisteredCommands();
const { remember } = useRecentPosts();

const post = ref<Post | null>(null);
const draft = ref<PostDraft | null>(null);
const loadError = ref("");
const missing = ref(false);
const saving = ref(false);
const acting = ref(false);
const saveError = ref("");
const conflict = ref<Post | null>(null);
const comparing = ref(false);
const recovered = ref<{ draft: PostDraft; version: number; at: number } | null>(null);
const detailsOpen = ref(false);
const historyOpen = ref(false);
const scheduleOpen = ref(false);
const confirm = reactive({
  open: false,
  title: "",
  body: "",
  label: "",
  color: "error" as "error" | "primary",
  run: () => {},
});
const justSaved = ref(false);
// Dialogs opened from the More menu give focus back to its button when they close.
const moreButton = ref<{ $el: HTMLElement } | null>(null);
const menuOpener = ref<HTMLElement | null>(null);
const fromMenu = (open: () => void) => () => {
  menuOpener.value = moreButton.value?.$el ?? null;
  open();
};
const closeFocus = returnFocusTo(() => {
  const opener = menuOpener.value;
  menuOpener.value = null;
  return opener;
});
// The Undo toast of the latest Unpublish or Archive; it must not outlive this page.
let undoToast: string | number | undefined;
onUnmounted(() => undoToast !== undefined && toast.remove(undoToast));
const shortcutsOpen = useState("shortcuts-open", () => false);
const { focused, toggle: toggleFocus } = useFocusedWriting();
const titleField = ref<HTMLTextAreaElement | null>(null);
const editor = ref<{
  focus: () => void;
  focusStart: () => void;
  element: HTMLTextAreaElement | null;
} | null>(null);

const editable = (p: Post): PostDraft => ({
  slug: p.slug,
  title: p.title,
  description: p.description,
  body: p.body,
  coverUrl: p.coverUrl,
  coverAlt: p.coverAlt,
  extra: p.extra,
  tags: [...p.tags],
  author: p.author,
  seoTitle: p.seoTitle,
  seoDescription: p.seoDescription,
  canonicalUrl: p.canonicalUrl,
  publishedOn: p.publishedOn,
  updatedOn: p.updatedOn,
});
const same = sameDraft;

const dirty = computed(() =>
  Boolean(post.value && draft.value && !same(draft.value, editable(post.value))),
);
watch(dirty, (value) => (dirtyFlag.value = value), { immediate: true });
const archived = computed(() => post.value?.status === "archived");
const scheduled = computed(() => Boolean(post.value && isScheduled(post.value)));
useHead({
  title: () =>
    missing.value
      ? "Post not found"
      : (draft.value?.title || post.value?.slug || "Post") + (dirty.value ? " •" : ""),
});

// Unsaved work is mirrored into this browser, so a crash or closed tab does
// not lose it. It is cleared on save or discard.
const backupKey = (slug: string) => `burzblog:unsaved:${slug}`;
let backupTimer: ReturnType<typeof setTimeout> | undefined;
watch(
  draft,
  () => {
    if (!post.value || !draft.value) return;
    clearTimeout(backupTimer);
    const key = backupKey(post.value.slug);
    const value = { draft: draft.value, version: post.value.version, at: Date.now() };
    backupTimer = setTimeout(() => {
      try {
        // A backup still on offer is never touched until the writer decides.
        if (recovered.value) return;
        if (dirty.value) localStorage.setItem(key, JSON.stringify(value));
        else localStorage.removeItem(key);
      } catch {
        // Private windows may refuse storage; the editor still works.
      }
    }, 600);
  },
  { deep: true },
);
const clearBackup = (slug: string) => {
  try {
    localStorage.removeItem(backupKey(slug));
  } catch {
    /* storage unavailable */
  }
};

const show = (p: Post) => {
  post.value = p;
  draft.value = editable(p);
};

const load = async () => {
  loadError.value = "";
  missing.value = false;
  const slug = String(route.params.slug);
  try {
    const p = await request<Post>(`/api/posts/${encodeURIComponent(slug)}`);
    show(p);
    remember(p.slug);
    try {
      const raw = localStorage.getItem(backupKey(slug));
      const backup = raw ? (JSON.parse(raw) as typeof recovered.value) : null;
      if (backup && !same(backup.draft, editable(p))) recovered.value = backup;
    } catch {
      /* storage unavailable or corrupt */
    }
    await nextTick();
    if (!p.body && !p.title) titleField.value?.focus();
  } catch (cause) {
    missing.value = cause instanceof ApiError && cause.code === "not_found";
    loadError.value = missing.value
      ? `Nothing is saved at “${slug}”. It may have been renamed or deleted.`
      : errorMessage(cause);
  }
};
onMounted(load);

// The old slugs that redirect to this post, shown in its settings.
const redirects = ref<string[]>([]);
watch(
  () => post.value?.slug,
  async (slug) => {
    if (!slug) return;
    try {
      const found = await request<{ items: string[] }>(
        `/api/posts/${encodeURIComponent(slug)}/redirects`,
      );
      if (post.value?.slug === slug) redirects.value = found.items;
    } catch {
      redirects.value = [];
    }
  },
  { immediate: true },
);
const removeRedirect = async (from: string) => {
  if (!post.value) return;
  try {
    await request(
      `/api/posts/${encodeURIComponent(post.value.slug)}/redirects/${encodeURIComponent(from)}`,
      "DELETE",
    );
    redirects.value = redirects.value.filter((old) => old !== from);
    toast.add({ title: "Redirect removed", color: "success" });
  } catch (cause) {
    toast.add({
      title: "Could not remove the redirect",
      description: errorMessage(cause),
      color: "error",
    });
  }
};
watch(loadError, (message) => (editorFailed.value = Boolean(message)));
onUnmounted(() => (editorFailed.value = false));

const restoreBackup = () => {
  if (!recovered.value || !post.value) return;
  // A backup from before tags and SEO existed lacks those fields.
  draft.value = { ...editable(post.value), ...recovered.value.draft };
  // Saving checks this version, so newer work on the server shows as a conflict.
  if (recovered.value.version !== post.value.version)
    post.value = { ...post.value, version: recovered.value.version };
  recovered.value = null;
};
const discardBackup = () => {
  if (post.value) clearBackup(post.value.slug);
  recovered.value = null;
};

const save = async (overwriteVersion?: number): Promise<boolean> => {
  if (!post.value || !draft.value || saving.value) return false;
  if (!dirty.value && overwriteVersion === undefined) return true;
  saving.value = true;
  saveError.value = "";
  const from = post.value.slug;
  const sent = { ...draft.value };
  try {
    const saved = await request<Post>(`/api/posts/${encodeURIComponent(from)}`, "PUT", {
      version: overwriteVersion ?? post.value.version,
      ...sent,
    });
    clearBackup(from);
    conflict.value = null;
    comparing.value = false;
    post.value = saved;
    // Take the server's copy (it trims stray spaces) unless the writer typed
    // more while the save was in flight; that stays as unsaved changes.
    if (same(draft.value, sent)) draft.value = editable(saved);
    if (saved.slug !== from) {
      remember(saved.slug);
      await navigateTo(`/posts/${saved.slug}`, { replace: true });
    }
    justSaved.value = true;
    setTimeout(() => (justSaved.value = false), 2400);
    return true;
  } catch (cause) {
    if (cause instanceof ApiError && cause.code === "conflict" && cause.current) {
      conflict.value = cause.current;
    } else {
      saveError.value = errorMessage(cause);
      if (cause instanceof ApiError && cause.detail.startsWith("slug "))
        detailsOpen.value = true;
    }
    return false;
  } finally {
    saving.value = false;
  }
};

const keepMine = () => conflict.value && save(conflict.value.version);
const useTheirs = () => {
  if (!conflict.value) return;
  clearBackup(conflict.value.slug);
  show(conflict.value);
  conflict.value = null;
  comparing.value = false;
};

const action = async (
  name: "publish" | "unpublish" | "archive" | "restore" | "schedule" | "unschedule",
  silent = false,
  extra: { at?: string } = {},
): Promise<boolean> => {
  if (!post.value) return false;
  if (dirty.value && !(await save())) return false;
  const wasPublished = post.value.status === "published";
  acting.value = true;
  try {
    const p = await request<Post>(
      `/api/posts/${encodeURIComponent(post.value.slug)}/${name}`,
      "POST",
      {
        version: post.value.version,
        ...extra,
      },
    );
    show(p);
    if (silent) return true;
    const messages = {
      publish: ["Published", "Sites with a deploy hook rebuild in a few seconds."],
      unpublish: ["Unpublished", "It will leave your site on the next build."],
      archive: ["Archived", "Find it under Archived on the posts list."],
      restore: ["Restored", "It is a draft again."],
      schedule: [
        "Scheduled",
        `${scheduledLabel(p.publishAt ?? "")}. Sites with a deploy hook rebuild then.`,
      ],
      unschedule: ["Unscheduled", "It is a plain draft again."],
    } as const;
    // Unpublish and Archive take effect at once, so they come with an Undo.
    // Undoing an Archive of a live post restores it, then puts it back on the site.
    const undoArchive = async () => {
      if (!wasPublished) await action("restore");
      else if (await action("restore", true)) await action("publish");
    };
    const undo =
      name === "unpublish"
        ? () => action("publish")
        : name === "archive"
          ? undoArchive
          : null;
    const added = toast.add({
      title: messages[name][0],
      description: messages[name][1],
      color: "success",
      icon: "i-lucide-check",
      ...(undo && {
        duration: 10000,
        actions: [{ label: "Undo", color: "neutral", variant: "outline", onClick: undo }],
      }),
    });
    undoToast = undo ? added.id : undefined;
    return true;
  } catch (cause) {
    if (cause instanceof ApiError && cause.code === "conflict" && cause.current)
      conflict.value = cause.current;
    else
      toast.add({
        title: "That did not work",
        description: errorMessage(cause),
        color: "error",
      });
    return false;
  } finally {
    acting.value = false;
  }
};

const ask = (opts: Omit<typeof confirm, "open">) =>
  Object.assign(confirm, opts, { open: true });

const publish = () =>
  ask({
    title: dirty.value ? "Save and publish this post?" : "Publish this post?",
    body: "It becomes part of your site on the next build. You can unpublish it later.",
    label: dirty.value ? "Save and publish" : "Publish",
    color: "primary",
    run: () => action("publish"),
  });

const schedule = async (at: string) => {
  if (await action("schedule", false, { at })) scheduleOpen.value = false;
};

const { isAdmin } = useCurrentSession();

const remove = () =>
  ask({
    title: "Delete this post for good?",
    body: "The post and its whole history are removed. This cannot be undone.",
    label: "Delete post",
    color: "error",
    run: async () => {
      if (!post.value) return;
      try {
        await request(
          `/api/posts/${encodeURIComponent(post.value.slug)}?version=${post.value.version}`,
          "DELETE",
        );
        clearBackup(post.value.slug);
        post.value = null;
        dirtyFlag.value = false;
        toast.add({ title: "Post deleted", color: "success" });
        await navigateTo("/?status=archived");
      } catch (cause) {
        toast.add({
          title: "Could not delete",
          description: errorMessage(cause),
          color: "error",
        });
      }
    },
  });

const restoreRevision = async (version: number) => {
  if (!post.value) return;
  try {
    const p = await request<Post>(
      `/api/posts/${encodeURIComponent(post.value.slug)}/revisions/${version}/restore`,
      "POST",
      { version: post.value.version },
    );
    clearBackup(p.slug);
    show(p);
    historyOpen.value = false;
    toast.add({
      title: `Restored version ${version}`,
      description: `Saved as version ${p.version}.`,
      color: "success",
    });
  } catch (cause) {
    if (cause instanceof ApiError && cause.code === "conflict" && cause.current) {
      historyOpen.value = false;
      conflict.value = cause.current;
    } else
      toast.add({
        title: "Could not restore",
        description: errorMessage(cause),
        color: "error",
      });
  }
};

const copyCommand = async () => {
  if (!post.value) return;
  try {
    await navigator.clipboard.writeText(`burzblog edit ${post.value.slug}`);
    toast.add({
      title: "Copied",
      description: `burzblog edit ${post.value.slug}`,
      color: "success",
    });
  } catch {
    toast.add({
      title: "Could not copy",
      description: `Run: burzblog edit ${post.value.slug}`,
      color: "neutral",
    });
  }
};
const download = async () => {
  if (!post.value) return;
  const response = await fetch(
    `/api/posts/${encodeURIComponent(post.value.slug)}/markdown`,
    {
      credentials: "same-origin",
    },
  );
  if (!response.ok) {
    toast.add({
      title: "Could not download",
      description: "Save the post, then try again.",
      color: "error",
    });
    return;
  }
  const url = URL.createObjectURL(await response.blob());
  const a = document.createElement("a");
  a.href = url;
  a.download = `${post.value.slug}.md`;
  a.click();
  URL.revokeObjectURL(url);
};

const menu = computed(() => {
  const p = post.value;
  if (!p) return [];
  const status = [];
  if (isScheduled(p))
    status.push({
      label: "Change schedule…",
      icon: "i-lucide-calendar-clock",
      onSelect: fromMenu(() => (scheduleOpen.value = true)),
    });
  if (p.status === "published")
    status.push({
      label: "Unpublish",
      icon: "i-lucide-eye-off",
      onSelect: () => action("unpublish"),
    });
  if (p.status !== "archived")
    status.push({
      label: "Archive",
      icon: "i-lucide-archive",
      onSelect: () => action("archive"),
    });
  if (p.status === "archived") {
    status.push({
      label: "Restore as draft",
      icon: "i-lucide-archive-restore",
      onSelect: () => action("restore"),
    });
    // Only admins may delete; the server refuses authors anyway.
    if (isAdmin.value)
      status.push({
        label: "Delete post…",
        icon: "i-lucide-trash-2",
        color: "error" as const,
        onSelect: fromMenu(remove),
      });
  }
  return [
    [
      {
        label: "Post settings",
        icon: "i-lucide-sliders-horizontal",
        onSelect: fromMenu(() => (detailsOpen.value = true)),
      },
      {
        label: "History",
        icon: "i-lucide-history",
        onSelect: fromMenu(() => (historyOpen.value = true)),
      },
    ],
    [
      {
        label: "Copy terminal command",
        icon: "i-lucide-terminal",
        onSelect: copyCommand,
      },
      { label: "Download .md", icon: "i-lucide-download", onSelect: download },
    ],
    [
      {
        label: "Keyboard shortcuts",
        icon: "i-lucide-keyboard",
        kbds: ["?"],
        onSelect: () => {
          shortcutsOpen.value = true;
        },
      },
    ],
    status,
  ];
});

// Save state, in words.
const saveState = computed(() => {
  if (saving.value) return { text: "Saving…", kind: "busy" };
  if (saveError.value || conflict.value) return { text: "Not saved", kind: "error" };
  if (dirty.value) return { text: "Unsaved changes", kind: "dirty" };
  if (justSaved.value) return { text: "Saved", kind: "saved" };
  return {
    text: post.value ? `Saved ${editedLabel(post.value.updatedAt).toLowerCase()}` : "",
    kind: "clean",
  };
});
const words = computed(() => countWords(draft.value?.body ?? ""));

// Layout: write, split or preview, remembered per browser.
type Mode = "write" | "split" | "preview";
const mode = ref<Mode>("split");
const ratio = ref(0.5);
const narrow = ref(false);
const media =
  typeof window === "undefined" ? null : window.matchMedia("(max-width: 899px)");
const syncNarrow = () => {
  narrow.value = Boolean(media?.matches);
  if (narrow.value && mode.value === "split") mode.value = "write";
};
onMounted(() => {
  try {
    const saved = JSON.parse(localStorage.getItem("burzblog:layout") ?? "{}") as {
      mode?: Mode;
      ratio?: number;
    };
    if (saved.mode) mode.value = saved.mode;
    if (saved.ratio && saved.ratio > 0.2 && saved.ratio < 0.8) ratio.value = saved.ratio;
  } catch {
    /* defaults */
  }
  syncNarrow();
  media?.addEventListener("change", syncNarrow);
});
onUnmounted(() => media?.removeEventListener("change", syncNarrow));
watch([mode, ratio], () => {
  try {
    localStorage.setItem(
      "burzblog:layout",
      JSON.stringify({ mode: mode.value, ratio: ratio.value }),
    );
  } catch {
    /* storage unavailable */
  }
});
// Focus mode writes in one centred column, whatever layout was chosen.
const layout = computed<Mode>(() => (focused.value ? "write" : mode.value));
// Entering or leaving focus mode removes the focused control, so the cursor
// goes back to the writing.
watch(focused, async () => {
  await nextTick();
  editor.value?.focus();
});
const modes = computed(() =>
  (narrow.value ? ["write", "preview"] : ["write", "split", "preview"]).map((m) => ({
    value: m as Mode,
    label: { write: "Write", split: "Split", preview: "Preview" }[m as Mode],
  })),
);

// What the command palette offers while this post is open. Every command goes
// through the same functions as the buttons, so the state machine, the confirm
// step and the Undo toast stay in one place.
const paletteCommands = (): PaletteCommand[] => {
  const p = post.value;
  if (!p) return [];
  const status: PaletteCommand[] = [];
  if (p.status === "draft") {
    status.push({
      id: "publish",
      label: scheduled.value ? "Publish now" : "Publish",
      icon: "i-lucide-send",
      run: publish,
    });
    status.push({
      id: "schedule",
      label: scheduled.value ? "Change schedule…" : "Schedule…",
      icon: "i-lucide-calendar-clock",
      run: () => (scheduleOpen.value = true),
    });
    if (scheduled.value)
      status.push({
        id: "unschedule",
        label: "Unschedule",
        icon: "i-lucide-calendar-x",
        run: () => void action("unschedule"),
      });
  } else if (p.status === "published")
    status.push({
      id: "unpublish",
      label: "Unpublish",
      icon: "i-lucide-eye-off",
      run: () => void action("unpublish"),
    });
  else
    status.push({
      id: "restore",
      label: "Restore as draft",
      icon: "i-lucide-archive-restore",
      run: () => void action("restore"),
    });
  const modeIcons = {
    write: "i-lucide-pen-line",
    split: "i-lucide-columns-2",
    preview: "i-lucide-eye",
  };
  return [
    ...(archived.value
      ? []
      : [
          {
            id: "save",
            label: "Save",
            icon: "i-lucide-save",
            kbds: ["meta", "s"],
            disabled: !dirty.value && !saving.value,
            run: () => void save(),
          },
        ]),
    ...status,
    {
      id: "settings",
      label: "Post settings",
      icon: "i-lucide-sliders-horizontal",
      run: () => (detailsOpen.value = true),
    },
    {
      id: "history",
      label: "History",
      icon: "i-lucide-history",
      run: () => (historyOpen.value = true),
    },
    {
      id: "focus",
      label: focused.value ? "Leave focus mode" : "Focus mode",
      icon: focused.value ? "i-lucide-minimize-2" : "i-lucide-maximize-2",
      kbds: ["meta", "shift", "f"],
      run: toggleFocus,
    },
    ...modes.value
      .filter((m) => m.value !== mode.value)
      .map((m) => ({
        id: `mode-${m.value}`,
        label: `${m.label} layout`,
        icon: modeIcons[m.value],
        run: () => (mode.value = m.value),
      })),
  ];
};
let unregister = () => {};
onMounted(() => (unregister = register(paletteCommands)));
onUnmounted(() => unregister());

// Dragging or arrow keys on the divider resize the split.
const panes = ref<HTMLElement | null>(null);
const startDrag = (event: PointerEvent) => {
  const el = panes.value;
  if (!el) return;
  (event.target as HTMLElement).setPointerCapture(event.pointerId);
  const move = (e: PointerEvent) => {
    const box = el.getBoundingClientRect();
    ratio.value = Math.min(0.75, Math.max(0.25, (e.clientX - box.left) / box.width));
  };
  const up = () => {
    window.removeEventListener("pointermove", move);
    window.removeEventListener("pointerup", up);
  };
  window.addEventListener("pointermove", move);
  window.addEventListener("pointerup", up);
};
const dividerKey = (event: KeyboardEvent) => {
  const step = event.shiftKey ? 0.1 : 0.02;
  if (event.key === "ArrowLeft") ratio.value = Math.max(0.25, ratio.value - step);
  else if (event.key === "ArrowRight") ratio.value = Math.min(0.75, ratio.value + step);
  else if (event.key === "Home") ratio.value = 0.25;
  else if (event.key === "End") ratio.value = 0.75;
  else return;
  event.preventDefault();
};

// The preview follows the writing pane's scroll position.
const scrollRatio = ref(0);
const writePane = ref<HTMLElement | null>(null);
const onWriteScroll = () => {
  const el = writePane.value;
  if (!el) return;
  const max = el.scrollHeight - el.clientHeight;
  scrollRatio.value = max > 0 ? el.scrollTop / max : 0;
};

// Title and description grow with their text.
const grow = (event: Event) => {
  const el = event.target as HTMLTextAreaElement;
  if (CSS.supports("field-sizing", "content")) return;
  el.style.height = "auto";
  el.style.height = `${el.scrollHeight}px`;
};
const titleKey = (event: KeyboardEvent) => {
  if (event.key === "Enter") {
    event.preventDefault();
    editor.value?.focusStart();
  }
};
// The description is one line: Enter moves on to the body, and pasted line
// breaks become spaces.
const descriptionKey = (event: KeyboardEvent) => {
  if (event.key === "Enter" && !event.isComposing) {
    event.preventDefault();
    editor.value?.focusStart();
  }
};
const descriptionInput = (event: Event) => {
  const el = event.target as HTMLTextAreaElement;
  if (/[\r\n]/.test(el.value) && draft.value)
    draft.value.description = el.value.replace(/\s*[\r\n]+\s*/g, " ");
  grow(event);
};

// Ctrl/Cmd+S saves from anywhere on the page, Ctrl/Cmd+Shift+F toggles focus
// mode, and Esc leaves it (unless a dialog or menu is open and wants the key).
const keydown = (event: KeyboardEvent) => {
  const key = event.key.toLowerCase();
  if ((event.metaKey || event.ctrlKey) && !event.altKey && key === "s") {
    event.preventDefault();
    void save();
  } else if ((event.metaKey || event.ctrlKey) && event.shiftKey && key === "f") {
    event.preventDefault();
    toggleFocus();
  } else if (key === "escape" && focused.value && !event.defaultPrevented) {
    const target = event.target as HTMLElement | null;
    if (target?.closest("[role=dialog], [role=menu]")) return;
    focused.value = false;
  }
};
const beforeUnload = (event: BeforeUnloadEvent) => {
  if (dirty.value) event.preventDefault();
};
onMounted(() => {
  window.addEventListener("keydown", keydown);
  window.addEventListener("beforeunload", beforeUnload);
});
onUnmounted(() => {
  window.removeEventListener("keydown", keydown);
  window.removeEventListener("beforeunload", beforeUnload);
  dirtyFlag.value = false;
});

onBeforeRouteLeave((to) => {
  if (!dirty.value || to.params.slug === draft.value?.slug) return true;
  return new Promise<boolean>((resolve) => {
    ask({
      title: "Leave without saving?",
      body: "Your changes stay in this browser, and BurzBlog offers them again next time you open this post.",
      label: "Leave",
      color: "error",
      run: () => resolve(true),
    });
    const stop = watch(
      () => confirm.open,
      (open) => {
        if (!open) {
          stop();
          resolve(false);
        }
      },
    );
  });
});
const runConfirm = () => {
  const run = confirm.run;
  confirm.run = () => {};
  run();
  confirm.open = false;
};

// The Publish button's extra choices, next to it as a split button.
const publishMore = ref<{ $el: HTMLElement } | null>(null);
const publishMenu = computed(() => [
  {
    label: "Schedule…",
    icon: "i-lucide-calendar-clock",
    onSelect: () => {
      menuOpener.value = publishMore.value?.$el ?? null;
      scheduleOpen.value = true;
    },
  },
]);

const primary = computed(() => {
  if (!post.value || archived.value) return null;
  if (dirty.value || saving.value) return "save";
  return post.value.status === "draft" ? "publish" : null;
});
</script>

<template>
  <div v-if="loadError" class="content">
    <div class="page-head">
      <div>
        <h1 class="page-title">
          {{ missing ? "Post not found" : "The post did not open" }}
        </h1>
        <p class="muted">{{ loadError }}</p>
      </div>
    </div>
    <div class="load-error-actions">
      <UButton to="/" color="neutral" variant="outline" icon="i-lucide-arrow-left"
        >All posts</UButton
      >
      <UButton v-if="!missing" color="neutral" variant="ghost" @click="load"
        >Try again</UButton
      >
    </div>
  </div>
  <p v-else-if="!post || !draft" class="loading" role="status">Opening the post…</p>
  <div v-else class="editor-shell" :data-mode="layout" :data-focus="focused">
    <h1 class="sr-only">Editing {{ draft.title || "Untitled" }}</h1>
    <header class="editor-bar">
      <UTooltip v-if="focused" text="Leave focus mode" :kbds="['escape']">
        <UButton
          icon="i-lucide-minimize-2"
          color="neutral"
          variant="ghost"
          class="focus-exit"
          aria-label="Leave focus mode"
          @click="focused = false"
        />
      </UTooltip>
      <NuxtLink to="/" class="editor-back" aria-label="All posts">
        <span class="brand-mark" aria-hidden="true" />
        <UIcon name="i-lucide-arrow-left" class="size-4" />
        <span class="max-md:sr-only">Posts</span>
      </NuxtLink>
      <div class="editor-crumb">
        <span class="editor-crumb-title">{{ draft.title || "Untitled" }}</span>
        <StatusBadge :status="post.status" :publish-at="post.publishAt" />
        <time
          v-if="scheduled && post.publishAt"
          class="schedule-note"
          :datetime="post.publishAt"
          :title="`${exactTime(post.publishAt)} (${zoneLabel()})`"
          >{{ publishesLabel(post.publishAt) }}</time
        >
      </div>
      <span
        class="save-state"
        :data-kind="saveState.kind"
        role="status"
        aria-live="polite"
      >
        <UIcon v-if="saveState.kind === 'saved'" name="i-lucide-check" class="size-4" />
        <span v-else-if="saveState.kind === 'dirty'" class="dot" aria-hidden="true" />
        {{ saveState.text }}
      </span>
      <div class="editor-actions">
        <UTooltip text="History">
          <UButton
            icon="i-lucide-history"
            color="neutral"
            variant="ghost"
            aria-label="History"
            class="max-sm:hidden"
            @click="historyOpen = true"
          />
        </UTooltip>
        <UTooltip text="Post settings">
          <UButton
            icon="i-lucide-sliders-horizontal"
            color="neutral"
            variant="ghost"
            aria-label="Post settings"
            class="max-sm:hidden"
            @click="detailsOpen = true"
          />
        </UTooltip>
        <UDropdownMenu :items="menu" :content="{ align: 'end' }">
          <UButton
            ref="moreButton"
            icon="i-lucide-ellipsis"
            color="neutral"
            variant="ghost"
            aria-label="More actions"
          />
        </UDropdownMenu>
        <UTooltip v-if="!archived" text="Save" :kbds="['meta', 's']">
          <UButton
            :color="primary === 'save' ? 'primary' : 'neutral'"
            :variant="primary === 'save' ? 'solid' : 'outline'"
            :loading="saving"
            :disabled="!dirty && !saving"
            @click="save()"
            >Save</UButton
          >
        </UTooltip>
        <template v-if="scheduled">
          <UButton
            color="neutral"
            variant="outline"
            :loading="acting"
            @click="action('unschedule')"
            >Unschedule</UButton
          >
          <UButton
            :color="primary === 'publish' ? 'primary' : 'secondary'"
            :variant="primary === 'publish' ? 'solid' : 'outline'"
            :loading="acting"
            @click="publish"
            >Publish now</UButton
          >
        </template>
        <UFieldGroup v-else-if="post.status === 'draft'">
          <UButton
            :color="primary === 'publish' ? 'primary' : 'secondary'"
            :variant="primary === 'publish' ? 'solid' : 'outline'"
            :loading="acting"
            @click="publish"
            >Publish</UButton
          >
          <UDropdownMenu :items="publishMenu" :content="{ align: 'end' }">
            <UButton
              ref="publishMore"
              :color="primary === 'publish' ? 'primary' : 'secondary'"
              :variant="primary === 'publish' ? 'solid' : 'outline'"
              icon="i-lucide-chevron-down"
              aria-label="More ways to publish"
            />
          </UDropdownMenu>
        </UFieldGroup>
        <UButton
          v-else-if="archived"
          color="primary"
          icon="i-lucide-archive-restore"
          :loading="acting"
          @click="action('restore')"
          >Restore</UButton
        >
      </div>
    </header>

    <div v-if="recovered" class="editor-notice" role="alert">
      <UIcon name="i-lucide-life-buoy" class="size-5 shrink-0" />
      <p>
        <strong
          >Unsaved changes from
          {{ editedLabel(new Date(recovered.at).toISOString()).toLowerCase() }} are still
          in this browser.</strong
        >
        Bring them back to keep working on them.
      </p>
      <div class="editor-notice-actions">
        <UButton color="neutral" variant="ghost" @click="discardBackup">Discard</UButton>
        <UButton color="secondary" @click="restoreBackup">Bring them back</UButton>
      </div>
    </div>

    <div v-if="conflict" class="editor-notice editor-notice-warn" role="alert">
      <UIcon name="i-lucide-git-compare" class="size-5 shrink-0" />
      <div class="min-w-0 flex-1">
        <p>
          <strong
            >{{ conflict.updatedBy || "Someone" }} saved a newer version
            {{ editedLabel(conflict.updatedAt).toLowerCase() }}.</strong
          >
          Your changes are not saved yet. Compare them, then choose which to keep.
        </p>
        <div v-if="comparing" class="mt-3">
          <p class="diff-legend">
            <span class="diff-key diff-removed">− only in yours</span>
            <span class="diff-key diff-added">+ only in theirs</span>
          </p>
          <div v-if="diffTooLong(draft.body, conflict.body)" class="quiet-block">
            These versions are too long to compare line by line.
          </div>
          <div
            v-else
            class="diff-panel"
            role="region"
            aria-label="Differences"
            tabindex="0"
          >
            <div
              v-for="(line, i) in lineDiff(draft.body, conflict.body)"
              :key="i"
              class="diff-line"
              :class="`diff-${line.kind}`"
            >
              <span class="diff-gutter" aria-hidden="true">{{
                line.kind === "added" ? "+" : line.kind === "removed" ? "−" : ""
              }}</span>
              <span class="diff-text">{{ line.text || " " }}</span>
            </div>
          </div>
        </div>
      </div>
      <div class="editor-notice-actions">
        <UButton color="neutral" variant="ghost" @click="comparing = !comparing">{{
          comparing ? "Hide comparison" : "Compare"
        }}</UButton>
        <UButton color="neutral" variant="outline" @click="useTheirs">Use theirs</UButton>
        <UButton color="secondary" :loading="saving" @click="keepMine">Keep mine</UButton>
      </div>
    </div>

    <p v-if="saveError" class="editor-notice editor-notice-error" role="alert">
      <UIcon name="i-lucide-circle-alert" class="size-5 shrink-0" />
      <span><strong>Not saved.</strong> {{ saveError }}</span>
    </p>

    <div v-if="archived" class="editor-notice">
      <UIcon name="i-lucide-archive" class="size-5 shrink-0" />
      <p>This post is archived, so it is read-only. Restore it to keep writing.</p>
    </div>

    <div class="editor-toolbar">
      <div class="editor-toolbar-start">
        <div class="segment" role="group" aria-label="Layout">
          <button
            v-for="m in modes"
            :key="m.value"
            type="button"
            class="segment-item"
            :aria-pressed="mode === m.value"
            @click="mode = m.value"
          >
            {{ m.label }}
          </button>
        </div>
        <UTooltip text="Focus mode" :kbds="['meta', 'shift', 'f']">
          <UButton
            icon="i-lucide-maximize-2"
            color="neutral"
            variant="ghost"
            size="sm"
            aria-label="Focus mode"
            @click="toggleFocus"
          />
        </UTooltip>
      </div>
      <span class="editor-count">{{ lengthLabel(words) }}</span>
    </div>

    <div
      ref="panes"
      class="editor-panes"
      :style="
        layout === 'split'
          ? { gridTemplateColumns: `${ratio}fr 9px ${1 - ratio}fr` }
          : undefined
      "
    >
      <section
        v-show="layout !== 'preview'"
        ref="writePane"
        class="write-pane"
        aria-label="Writing"
        @scroll.passive="onWriteScroll"
      >
        <div class="write-column">
          <textarea
            id="post-title"
            ref="titleField"
            v-model="draft.title"
            class="title-field"
            rows="1"
            placeholder="Title"
            aria-label="Title"
            maxlength="300"
            :readonly="archived"
            @input="grow"
            @keydown="titleKey"
          />
          <textarea
            id="post-description"
            v-model="draft.description"
            class="description-field"
            rows="1"
            placeholder="A one-line description for link previews and search results"
            aria-label="Description"
            maxlength="2000"
            :readonly="archived"
            @input="descriptionInput"
            @keydown="descriptionKey"
          />
          <MarkdownEditor ref="editor" v-model="draft.body" :readonly="archived" />
        </div>
      </section>
      <div
        v-if="layout === 'split'"
        class="pane-divider"
        role="separator"
        aria-orientation="vertical"
        aria-label="Resize writing and preview"
        :aria-valuenow="Math.round(ratio * 100)"
        aria-valuemin="25"
        aria-valuemax="75"
        tabindex="0"
        @pointerdown="startDrag"
        @keydown="dividerKey"
      />
      <PostPreview
        v-show="layout !== 'write'"
        :body="draft.body"
        :dark="themeDark"
        :meta="{
          title: draft.title,
          description: draft.description,
          coverUrl: draft.coverUrl,
          coverAlt: draft.coverAlt,
        }"
        :scroll="layout === 'split' ? scrollRatio : undefined"
      />
    </div>

    <footer class="editor-foot muted">
      Version {{ post.version }}, last saved {{ exactTime(post.updatedAt)
      }}<template v-if="post.updatedBy"> by {{ post.updatedBy }}</template
      >.
      <template v-if="post.status === 'published' && post.publishedOn">
        Published {{ exactTime(post.publishedOn) }}.</template
      >
      <template v-if="scheduled && post.publishAt">
        Publishes {{ exactTime(post.publishAt) }}, {{ zoneLabel() }}.</template
      >
    </footer>

    <PostDetails
      v-model:open="detailsOpen"
      v-model:draft="draft"
      :status="post.status"
      :saved-slug="post.slug"
      :readonly="archived"
      :redirects="redirects"
      :close-focus="closeFocus"
      @remove-redirect="removeRedirect"
    />
    <HistoryPanel
      v-model:open="historyOpen"
      :slug="post.slug"
      :version="post.version"
      :body="draft.body"
      :dirty="dirty"
      :close-focus="closeFocus"
      @restore="restoreRevision"
    />
    <ScheduleDialog
      v-model:open="scheduleOpen"
      :current="post.publishAt"
      :busy="acting"
      :close-focus="closeFocus"
      @confirm="schedule"
    />
    <ConfirmDialog
      v-model:open="confirm.open"
      :title="confirm.title"
      :body="confirm.body"
      :confirm-label="confirm.label"
      :confirm-color="confirm.color"
      :close-focus="closeFocus"
      @confirm="runConfirm"
    />
  </div>
</template>
