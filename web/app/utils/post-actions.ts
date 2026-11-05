import type { Post, PostStatus } from "../types";
import { ApiError, errorMessage, request } from "./api";
import { maxTags, normalizeTag } from "./metadata";

export type PostAction = "publish" | "unpublish" | "archive" | "restore" | "delete";

export const actionOrder: PostAction[] = [
  "publish",
  "unpublish",
  "archive",
  "restore",
  "delete",
];

export const actionLabels: Record<PostAction, string> = {
  publish: "Publish",
  unpublish: "Unpublish",
  archive: "Archive",
  restore: "Restore",
  delete: "Delete",
};

const pastTense: Record<PostAction, string> = {
  publish: "Published",
  unpublish: "Unpublished",
  archive: "Archived",
  restore: "Restored",
  delete: "Deleted",
};

// Only a draft can go live, only a live post can come down, and only an
// archived post can be restored or (by an admin) deleted. The server decides
// in the end; this keeps the menus and the bulk bar to what can work.
export const actionApplies = (
  action: PostAction,
  post: { status: PostStatus },
  admin: boolean,
): boolean => {
  switch (action) {
    case "publish":
      return post.status === "draft";
    case "unpublish":
      return post.status === "published";
    case "archive":
      return post.status !== "archived";
    case "restore":
      return post.status === "archived";
    case "delete":
      return admin && post.status === "archived";
  }
};

// The actions that suit at least one of the posts, in menu order.
export const availableActions = (
  posts: { status: PostStatus }[],
  admin: boolean,
): PostAction[] =>
  actionOrder.filter((a) => posts.some((p) => actionApplies(a, p, admin)));

// Splits a selection into the posts the action suits and the ones to skip.
export const partitionByAction = <T extends { status: PostStatus }>(
  action: PostAction,
  posts: T[],
  admin: boolean,
): { apply: T[]; skipped: T[] } => ({
  apply: posts.filter((p) => actionApplies(action, p, admin)),
  skipped: posts.filter((p) => !actionApplies(action, p, admin)),
});

export interface BulkFailure {
  slug: string;
  message: string;
}

export interface BulkOutcome<T, R> {
  done: { item: T; result: R }[];
  failed: BulkFailure[];
}

// Runs one post at a time, so each change sees the one before it, and keeps
// going past a failure so the writer gets one list of what did not work.
export const runSequentially = async <T extends { slug: string }, R>(
  items: T[],
  run: (item: T) => Promise<R>,
): Promise<BulkOutcome<T, R>> => {
  const outcome: BulkOutcome<T, R> = { done: [], failed: [] };
  for (const item of items) {
    try {
      outcome.done.push({ item, result: await run(item) });
    } catch (cause) {
      outcome.failed.push({
        slug: item.slug,
        message:
          cause instanceof ApiError && cause.code === "conflict"
            ? "It changed since this list loaded."
            : errorMessage(cause),
      });
    }
  }
  return outcome;
};

const shown = 3;

// The toast text for a finished run. `names` are the posts that worked.
export const outcomeSummary = (
  verb: string,
  names: string[],
  failed: BulkFailure[],
  skipped = 0,
  why = "not applicable to their status",
): { title: string; description: string; color: "success" | "warning" | "error" } => {
  const lines: string[] = [];
  if (skipped) lines.push(`${skipped} skipped: ${why}.`);
  lines.push(
    ...failed.slice(0, shown).map((f) => `${f.slug}: ${f.message}`),
    ...(failed.length > shown ? [`and ${failed.length - shown} more failed.`] : []),
  );
  const what =
    names.length === 1
      ? `“${names[0]}”`
      : `${names.length} post${names.length === 1 ? "" : "s"}`;
  return {
    title: names.length ? `${verb} ${what}` : "Nothing changed",
    description: lines.join("\n"),
    color: failed.length ? (names.length ? "warning" : "error") : "success",
  };
};

export const actionSummary = (
  action: PostAction,
  names: string[],
  failed: BulkFailure[],
  skipped = 0,
) => outcomeSummary(pastTense[action], names, failed, skipped);

// The change itself, against the version the list was loaded with.
export const applyAction = (
  slug: string,
  action: PostAction,
  version: number,
): Promise<Post | { deleted: boolean }> =>
  action === "delete"
    ? request(`/api/posts/${encodeURIComponent(slug)}?version=${version}`, "DELETE")
    : request<Post>(`/api/posts/${encodeURIComponent(slug)}/${action}`, "POST", {
        version,
      });

// Publish, Unpublish and Archive take effect at once, so they come with an Undo.
export const hasUndo = (action: PostAction): boolean =>
  action === "publish" || action === "unpublish" || action === "archive";

// Undoes one finished action, from the post the action returned. Undoing a
// Publish unpublishes it; undoing an Archive of a live post restores it, then
// puts it back on the site.
export const reverseAction = async (
  action: PostAction,
  wasPublished: boolean,
  after: Post,
): Promise<Post> => {
  if (action === "publish")
    return (await applyAction(after.slug, "unpublish", after.version)) as Post;
  if (action === "unpublish")
    return (await applyAction(after.slug, "publish", after.version)) as Post;
  const restored = (await applyAction(after.slug, "restore", after.version)) as Post;
  return wasPublished
    ? ((await applyAction(restored.slug, "publish", restored.version)) as Post)
    : restored;
};

// Adds one tag through the normal save, so the post's other fields (including
// frontmatter BurzBlog does not model) go back exactly as the server has them.
// Resolves to null when the post already carries the tag.
export const addTag = async (slug: string, raw: string): Promise<Post | null> => {
  const tag = normalizeTag(raw);
  const path = `/api/posts/${encodeURIComponent(slug)}`;
  const current = await request<Post>(path);
  if (current.tags.includes(tag)) return null;
  if (current.tags.length >= maxTags)
    throw new Error(`It already has ${maxTags} tags, the most a post can carry.`);
  return request<Post>(path, "PUT", {
    version: current.version,
    slug: current.slug,
    title: current.title,
    description: current.description,
    body: current.body,
    coverUrl: current.coverUrl,
    coverAlt: current.coverAlt,
    extra: current.extra,
    tags: [...current.tags, tag],
    author: current.author,
    seoTitle: current.seoTitle,
    seoDescription: current.seoDescription,
    canonicalUrl: current.canonicalUrl,
    featured: current.featured,
    publishedOn: current.publishedOn,
    updatedOn: current.updatedOn,
  });
};
