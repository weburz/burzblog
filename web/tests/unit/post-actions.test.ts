import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, rememberSession } from "../../app/utils/api";
import {
  actionApplies,
  actionSummary,
  addTag,
  availableActions,
  outcomeSummary,
  partitionByAction,
  reverseAction,
  runSequentially,
} from "../../app/utils/post-actions";
import type { Post } from "../../app/types";

afterEach(() => vi.unstubAllGlobals());

const draft = { slug: "a", status: "draft" as const };
const live = { slug: "b", status: "published" as const };
const old = { slug: "c", status: "archived" as const };

describe("actionApplies", () => {
  it("offers each action only for the statuses it suits", () => {
    expect(actionApplies("publish", draft, false)).toBe(true);
    expect(actionApplies("publish", live, false)).toBe(false);
    expect(actionApplies("unpublish", live, false)).toBe(true);
    expect(actionApplies("unpublish", draft, false)).toBe(false);
    expect(actionApplies("archive", draft, false)).toBe(true);
    expect(actionApplies("archive", old, false)).toBe(false);
    expect(actionApplies("restore", old, false)).toBe(true);
    expect(actionApplies("restore", live, false)).toBe(false);
  });
  it("lets only admins delete, and only archived posts", () => {
    expect(actionApplies("delete", old, true)).toBe(true);
    expect(actionApplies("delete", old, false)).toBe(false);
    expect(actionApplies("delete", draft, true)).toBe(false);
  });
});

describe("availableActions and partitionByAction", () => {
  it("lists actions valid for at least one selected post, in order", () => {
    expect(availableActions([draft, live], false)).toEqual([
      "publish",
      "unpublish",
      "archive",
    ]);
    expect(availableActions([old], true)).toEqual(["restore", "delete"]);
    expect(availableActions([], true)).toEqual([]);
  });
  it("splits a selection into the posts to change and the ones to skip", () => {
    expect(partitionByAction("publish", [draft, live, old], false)).toEqual({
      apply: [draft],
      skipped: [live, old],
    });
  });
});

describe("runSequentially", () => {
  it("runs in order, one at a time, and collects results", async () => {
    const order: string[] = [];
    const out = await runSequentially([draft, live], async (p) => {
      order.push(`start ${p.slug}`);
      await Promise.resolve();
      order.push(`end ${p.slug}`);
      return p.slug.toUpperCase();
    });
    expect(order).toEqual(["start a", "end a", "start b", "end b"]);
    expect(out.done.map((d) => d.result)).toEqual(["A", "B"]);
    expect(out.failed).toEqual([]);
  });
  it("keeps going after a failure and says which post failed", async () => {
    const out = await runSequentially([draft, live, old], async (p) => {
      if (p.slug === "b") throw new Error("boom");
      if (p.slug === "c") throw new ApiError(409, "conflict", "Changed.", "", null);
      return p.slug;
    });
    expect(out.done.map((d) => d.item.slug)).toEqual(["a"]);
    expect(out.failed).toEqual([
      { slug: "b", message: "boom" },
      { slug: "c", message: "It changed since this list loaded." },
    ]);
  });
});

describe("outcomeSummary", () => {
  it("names a single post and a count of several", () => {
    expect(actionSummary("publish", ["Hello"], []).title).toBe("Published “Hello”");
    expect(actionSummary("archive", ["A", "B"], []).title).toBe("Archived 2 posts");
  });
  it("warns about partial failure and lists the failures and skips", () => {
    const s = outcomeSummary(
      "Published",
      ["A"],
      [
        { slug: "x", message: "no" },
        { slug: "y", message: "no" },
        { slug: "z", message: "no" },
        { slug: "w", message: "no" },
      ],
      2,
    );
    expect(s.color).toBe("warning");
    expect(s.description).toContain("2 skipped");
    expect(s.description).toContain("x: no");
    expect(s.description).toContain("and 1 more failed.");
  });
  it("is an error when nothing worked and says so when nothing changed", () => {
    expect(outcomeSummary("Published", [], [{ slug: "x", message: "no" }]).color).toBe(
      "error",
    );
    expect(outcomeSummary("Tagged", [], []).title).toBe("Nothing changed");
  });
});

const post = (over: Partial<Post> = {}): Post => ({
  slug: "a b",
  title: "T",
  description: "",
  status: "draft",
  body: "Body",
  coverUrl: "",
  coverAlt: "",
  extra: "mood: calm\n",
  tags: ["one"],
  author: "",
  seoTitle: "",
  seoDescription: "",
  canonicalUrl: "",
  featured: true,
  publishedOn: null,
  updatedOn: null,
  publishAt: null,
  createdAt: "",
  updatedAt: "",
  updatedBy: "",
  version: 4,
  ...over,
});

describe("addTag", () => {
  it("saves the whole post back with the tag added and the version it just read", async () => {
    rememberSession({ authenticated: true, csrf: "token" });
    const fetch = vi
      .fn<typeof globalThis.fetch>()
      .mockResolvedValueOnce(Response.json(post()))
      .mockResolvedValueOnce(Response.json(post({ version: 5 })));
    vi.stubGlobal("fetch", fetch);
    await addTag("a b", "Machine Learning");
    expect(fetch.mock.calls[1]?.[0]).toBe("/api/posts/a%20b");
    expect(fetch.mock.calls[1]?.[1]).toMatchObject({ method: "PUT" });
    const sent = JSON.parse(fetch.mock.calls[1]?.[1]?.body as string);
    expect(sent).toMatchObject({
      version: 4,
      extra: "mood: calm\n",
      body: "Body",
      featured: true,
      tags: ["one", "machine-learning"],
    });
    expect(sent.status).toBeUndefined();
  });
  it("does not save a post that already has the tag", async () => {
    rememberSession({ authenticated: true, csrf: "token" });
    const fetch = vi
      .fn<typeof globalThis.fetch>()
      .mockImplementation(async () => Response.json(post()));
    vi.stubGlobal("fetch", fetch);
    await expect(addTag("a", "One")).resolves.toBeNull();
    expect(fetch).toHaveBeenCalledTimes(1);
  });
  it("refuses a post that is out of tag room", async () => {
    rememberSession({ authenticated: true, csrf: "token" });
    const tags = Array.from({ length: 20 }, (_, i) => `t${i}`);
    vi.stubGlobal(
      "fetch",
      vi
        .fn<typeof globalThis.fetch>()
        .mockImplementation(async () => Response.json(post({ tags }))),
    );
    await expect(addTag("a", "extra")).rejects.toThrow(/20 tags/);
  });
});

describe("reverseAction", () => {
  it("undoes an Unpublish by publishing again", async () => {
    rememberSession({ authenticated: true, csrf: "token" });
    const fetch = vi
      .fn<typeof globalThis.fetch>()
      .mockImplementation(async () => Response.json(post({ status: "published" })));
    vi.stubGlobal("fetch", fetch);
    await reverseAction("unpublish", true, post({ slug: "a", version: 7 }));
    expect(fetch.mock.calls).toHaveLength(1);
    expect(fetch.mock.calls[0]?.[0]).toBe("/api/posts/a/publish");
  });
  it("undoes an Archive of a live post by restoring then publishing", async () => {
    rememberSession({ authenticated: true, csrf: "token" });
    const fetch = vi
      .fn<typeof globalThis.fetch>()
      .mockImplementation(async () => Response.json(post({ slug: "a", version: 8 })));
    vi.stubGlobal("fetch", fetch);
    await reverseAction("archive", true, post({ slug: "a", version: 7 }));
    expect(fetch.mock.calls.map((c) => c[0])).toEqual([
      "/api/posts/a/restore",
      "/api/posts/a/publish",
    ]);
    expect(JSON.parse(fetch.mock.calls[1]?.[1]?.body as string)).toEqual({ version: 8 });
  });
  it("undoes an Archive of a draft by restoring only", async () => {
    rememberSession({ authenticated: true, csrf: "token" });
    const fetch = vi
      .fn<typeof globalThis.fetch>()
      .mockImplementation(async () => Response.json(post({ slug: "a" })));
    vi.stubGlobal("fetch", fetch);
    await reverseAction("archive", false, post({ slug: "a" }));
    expect(fetch.mock.calls.map((c) => c[0])).toEqual(["/api/posts/a/restore"]);
  });
});
