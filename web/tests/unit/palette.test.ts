import { afterEach, describe, expect, it, vi } from "vitest";
import type { PostSummary } from "../../app/types";
import {
  actionCommands,
  goToCommands,
  opensPalette,
  paletteGroups,
  parseRecent,
  pushRecent,
  recentLimit,
  resolveRecent,
  type PaletteCommand,
} from "../../app/utils/palette";

const post = (slug: string, title = slug): PostSummary => ({
  slug,
  title,
  description: "",
  status: "draft",
  words: 0,
  publishedOn: null,
  updatedAt: "2026-10-01T10:00:00Z",
  updatedBy: "ada",
  version: 1,
});

const cmd = (id: string): PaletteCommand => ({
  id,
  label: id,
  icon: "i-lucide-check",
  run: () => {},
});

const groupsFor = (query: string, extra: Partial<Parameters<typeof paletteGroups>[0]>) =>
  paletteGroups({
    query,
    posts: [],
    recent: [],
    contextual: [],
    goTo: [cmd("g")],
    actions: [cmd("a")],
    run: (fn) => fn(),
    open: () => {},
    ...extra,
  });

afterEach(() => vi.restoreAllMocks());

describe("recent posts", () => {
  it("puts the newest first without duplicates", () => {
    expect(pushRecent(["a", "b", "c"], "b")).toEqual(["b", "a", "c"]);
  });

  it("keeps at most five", () => {
    let list: string[] = [];
    for (const slug of ["a", "b", "c", "d", "e", "f", "g"]) list = pushRecent(list, slug);
    expect(list).toHaveLength(recentLimit);
    expect(list).toEqual(["g", "f", "e", "d", "c"]);
  });

  it("reads a saved list and rejects anything else", () => {
    expect(parseRecent('["a","b"]')).toEqual(["a", "b"]);
    expect(parseRecent('["a",3,"",null]')).toEqual(["a"]);
    expect(parseRecent('{"a":1}')).toEqual([]);
    expect(parseRecent("not json")).toEqual([]);
    expect(parseRecent(null)).toEqual([]);
  });

  it("drops posts that no longer exist, keeping recent order", () => {
    const rows = resolveRecent(["gone", "b", "a"], [post("a"), post("b")]);
    expect(rows.map((r) => r.slug)).toEqual(["b", "a"]);
  });
});

describe("palette groups", () => {
  it("leads with recent posts when nothing is typed", () => {
    const groups = groupsFor("", { posts: [post("a"), post("b")], recent: ["b"] });
    expect(groups.map((g) => g.id)).toEqual(["recent", "goto", "actions"]);
    expect(groups[0]?.items.map((i) => i.suffix)).toEqual(["b"]);
  });

  it("leads with unfiltered server matches when searching", () => {
    const groups = groupsFor("zzz", {
      posts: [post("a", "Nothing alike")],
      recent: ["a"],
    });
    expect(groups[0]).toMatchObject({ id: "posts", label: "Posts", ignoreFilter: true });
    expect(groups[0]?.items).toHaveLength(1);
  });

  it("leaves out empty groups", () => {
    const groups = groupsFor("", { goTo: [], actions: [] });
    expect(groups).toEqual([]);
  });

  it("adds the open post's commands as their own group", () => {
    const groups = groupsFor("", { contextual: [cmd("save")] });
    expect(groups.map((g) => g.id)).toEqual(["post", "goto", "actions"]);
  });

  it("opens a post by slug and runs a command through run", () => {
    const open = vi.fn<(slug: string) => void>();
    const run = vi.fn<(fn: () => void) => void>((fn) => fn());
    const picked = vi.fn<() => void>();
    const groups = groupsFor("a", {
      posts: [post("a")],
      goTo: [{ ...cmd("g"), run: picked }],
      open,
      run,
    });
    const select = (group: number) =>
      (groups[group]!.items[0] as { onSelect: () => void }).onSelect();
    select(0);
    expect(open).toHaveBeenCalledWith("a");
    select(1);
    expect(run).toHaveBeenCalledOnce();
    expect(picked).toHaveBeenCalledOnce();
  });

  it("titles an untitled post", () => {
    const groups = groupsFor("a", { posts: [post("a", "")] });
    expect(groups[0]?.items[0]?.label).toBe("Untitled");
  });
});

describe("built-in commands", () => {
  it("go to the list filters and settings", () => {
    const go = vi.fn<(path: string) => void>();
    for (const command of goToCommands(go)) command.run();
    expect(go.mock.calls.map(([path]) => path)).toEqual([
      "/",
      "/?status=draft",
      "/?status=published",
      "/?status=archived",
      "/settings",
    ]);
  });

  it("offer every theme, new post, shortcuts and sign out", () => {
    const handlers = {
      newPost: vi.fn<() => void>(),
      theme: vi.fn<(value: string) => void>(),
      shortcuts: vi.fn<() => void>(),
      signOut: vi.fn<() => void>(),
    };
    const commands = actionCommands(handlers);
    expect(commands.map((c) => c.id)).toEqual([
      "new-post",
      "theme-system",
      "theme-light",
      "theme-dark",
      "shortcuts",
      "sign-out",
    ]);
    commands.find((c) => c.id === "theme-dark")?.run();
    expect(handlers.theme).toHaveBeenCalledWith("dark");
  });
});

describe("palette shortcut", () => {
  const key = (init: Partial<KeyboardEventInit> & { key: string }) => ({
    metaKey: false,
    ctrlKey: false,
    altKey: false,
    shiftKey: false,
    ...init,
  });

  it("opens on Ctrl or Cmd+K outside the body", () => {
    expect(opensPalette(key({ key: "k", ctrlKey: true }), false)).toBe(true);
    expect(opensPalette(key({ key: "K", metaKey: true }), false)).toBe(true);
  });

  it("leaves Ctrl or Cmd+K to the link shortcut in the body", () => {
    expect(opensPalette(key({ key: "k", ctrlKey: true }), true)).toBe(false);
    expect(opensPalette(key({ key: "k", metaKey: true }), true)).toBe(false);
  });

  it("opens on Ctrl or Cmd+Shift+K from anywhere, body included", () => {
    expect(opensPalette(key({ key: "K", ctrlKey: true, shiftKey: true }), true)).toBe(
      true,
    );
    expect(opensPalette(key({ key: "K", metaKey: true, shiftKey: true }), false)).toBe(
      true,
    );
  });

  it("ignores other keys, a bare K and Alt combinations", () => {
    expect(opensPalette(key({ key: "j", ctrlKey: true }), false)).toBe(false);
    expect(opensPalette(key({ key: "k" }), false)).toBe(false);
    expect(opensPalette(key({ key: "k", ctrlKey: true, altKey: true }), false)).toBe(
      false,
    );
  });
});
