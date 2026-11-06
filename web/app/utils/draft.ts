import type { Post, PostDraft } from "../types";
import { ApiError } from "./api";

const blank = (...texts: string[]): boolean => texts.every((text) => text.trim() === "");

// Whether a never-published draft holds nothing at all, the saved post p and
// the editor's draft d alike: every writable field blank after trimming. It
// mirrors the server's store.IsEmptyDraft, which has the last word. An author
// that is just the account that saved the draft is blank, because creating a
// draft credits its creator.
export const isEmptyDraft = (p: Post, d: PostDraft): boolean => {
  if (p.status !== "draft" || p.publishedOn) return false;
  const author = (a: string) => (a.trim() === p.updatedBy.trim() ? "" : a);
  return [p, d].every(
    (x) =>
      !x.featured &&
      x.tags.length === 0 &&
      blank(
        x.title,
        x.body,
        x.description,
        x.coverUrl,
        x.coverAlt,
        x.extra,
        x.seoTitle,
        x.seoDescription,
        x.canonicalUrl,
        author(x.author),
      ),
  );
};

// A failed save that trying again later can fix: no network, a server error,
// or a session that ended. A validation error or a conflict needs the writer.
export const isTransientFailure = (cause: unknown): boolean =>
  cause instanceof ApiError &&
  (cause.status === 0 || cause.status >= 500 || cause.status === 401);

// How long a save waits before its one retry, after the network is back or
// the writer signed in again.
export const retryBackoff = 1500;
