import type { PostSummary } from "../types";
import { themeOptions, type ThemePreference } from "./theme";

export const recentKey = "burzblog:recent";
export const recentLimit = 5;

// One thing the palette can do. `run` happens after the palette has closed.
export interface PaletteCommand {
  id: string;
  label: string;
  icon: string;
  kbds?: string[];
  disabled?: boolean;
  run: () => void;
}

export interface PaletteGroup {
  id: string;
  label: string;
  slot?: string;
  ignoreFilter?: boolean;
  items: Record<string, unknown>[];
}

// Saved slugs, newest first; anything that is not a list of strings is dropped.
export const parseRecent = (raw: string | null): string[] => {
  try {
    const value: unknown = JSON.parse(raw ?? "[]");
    if (!Array.isArray(value)) return [];
    return value
      .filter((slug): slug is string => typeof slug === "string" && slug !== "")
      .slice(0, recentLimit);
  } catch {
    return [];
  }
};

// The slug moves to the front; the list never grows past the limit.
export const pushRecent = (list: string[], slug: string): string[] =>
  [slug, ...list.filter((s) => s !== slug)].slice(0, recentLimit);

// Recent slugs as post rows, in recent order; posts that no longer exist drop out.
export const resolveRecent = (slugs: string[], posts: PostSummary[]): PostSummary[] =>
  slugs.flatMap((slug) => posts.filter((post) => post.slug === slug));

export const goToCommands = (go: (path: string) => void): PaletteCommand[] => [
  { id: "go-all", label: "All posts", icon: "i-lucide-files", run: () => go("/") },
  {
    id: "go-drafts",
    label: "Drafts",
    icon: "i-lucide-file-pen",
    run: () => go("/?status=draft"),
  },
  {
    id: "go-published",
    label: "Published",
    icon: "i-lucide-globe",
    run: () => go("/?status=published"),
  },
  {
    id: "go-archived",
    label: "Archived",
    icon: "i-lucide-archive",
    run: () => go("/?status=archived"),
  },
  {
    id: "go-settings",
    label: "Settings",
    icon: "i-lucide-settings",
    run: () => go("/settings"),
  },
];

export const actionCommands = (handlers: {
  newPost: () => void;
  theme: (value: ThemePreference) => void;
  shortcuts: () => void;
  signOut: () => void;
}): PaletteCommand[] => [
  { id: "new-post", label: "New post", icon: "i-lucide-plus", run: handlers.newPost },
  ...themeOptions.map((option) => ({
    id: `theme-${option.value}`,
    label: `Theme: ${option.label}`,
    icon: option.icon,
    run: () => handlers.theme(option.value),
  })),
  {
    id: "shortcuts",
    label: "Keyboard shortcuts",
    icon: "i-lucide-keyboard",
    kbds: ["?"],
    run: handlers.shortcuts,
  },
  { id: "sign-out", label: "Sign out", icon: "i-lucide-log-out", run: handlers.signOut },
];

const commandItem = (command: PaletteCommand, run: (fn: () => void) => void) => ({
  label: command.label,
  icon: command.icon,
  kbds: command.kbds,
  disabled: command.disabled,
  onSelect: () => run(command.run),
});

const postItem = (post: PostSummary, open: (slug: string) => void) => ({
  label: post.title || "Untitled",
  suffix: post.slug,
  status: post.status,
  updatedAt: post.updatedAt,
  onSelect: () => open(post.slug),
});

// The palette's groups, in the order they show. With nothing typed, recent
// posts lead; with a query, the server's matches do (they are already
// filtered, so the palette must not filter them again).
export const paletteGroups = (input: {
  query: string;
  posts: PostSummary[];
  recent: string[];
  contextual: PaletteCommand[];
  goTo: PaletteCommand[];
  actions: PaletteCommand[];
  run: (fn: () => void) => void;
  open: (slug: string) => void;
}): PaletteGroup[] => {
  const searching = input.query.trim() !== "";
  const rows = searching ? input.posts : resolveRecent(input.recent, input.posts);
  const commands = (id: string, label: string, list: PaletteCommand[]) => ({
    id,
    label,
    items: list.map((command) => commandItem(command, input.run)),
  });
  const groups: PaletteGroup[] = [
    {
      id: searching ? "posts" : "recent",
      label: searching ? "Posts" : "Recent",
      slot: "post",
      ignoreFilter: true,
      items: rows.map((post) => postItem(post, input.open)),
    },
    commands("post", "This post", input.contextual),
    commands("goto", "Go to", input.goTo),
    commands("actions", "Actions", input.actions),
  ];
  return groups.filter((group) => group.items.length > 0);
};
