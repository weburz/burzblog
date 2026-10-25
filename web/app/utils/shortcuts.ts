// Every keyboard shortcut, for the help dialog. "meta" renders as Cmd on Mac
// and Ctrl elsewhere (UKbd detects the platform once).
export interface Shortcut {
  label: string;
  keys: string[];
}

export interface ShortcutGroup {
  title: string;
  items: Shortcut[];
}

export const shortcutGroups: ShortcutGroup[] = [
  {
    title: "Anywhere",
    items: [
      { label: "Search or jump", keys: ["meta", "k"] },
      {
        label: "Search or jump, also while writing the body",
        keys: ["meta", "shift", "k"],
      },
      { label: "Show keyboard shortcuts", keys: ["?"] },
    ],
  },
  {
    title: "Posts list",
    items: [
      { label: "New post", keys: ["n"] },
      { label: "Search", keys: ["/"] },
    ],
  },
  {
    title: "Editor",
    items: [
      { label: "Save", keys: ["meta", "s"] },
      { label: "Focus mode", keys: ["meta", "shift", "f"] },
      { label: "Leave focus mode", keys: ["escape"] },
    ],
  },
  {
    title: "Formatting",
    items: [
      { label: "Bold", keys: ["meta", "b"] },
      { label: "Italic", keys: ["meta", "i"] },
      { label: "Link", keys: ["meta", "k"] },
      { label: "Inline code", keys: ["meta", "e"] },
    ],
  },
  {
    title: "Lists",
    items: [
      { label: "Continue the list on the next line", keys: ["enter"] },
      { label: "End the list on an empty item", keys: ["enter"] },
      { label: "Indent the item", keys: ["tab"] },
      { label: "Outdent the item", keys: ["shift", "tab"] },
    ],
  },
];
