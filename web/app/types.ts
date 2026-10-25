export type PostStatus = "draft" | "published" | "archived";

export interface Post {
  slug: string;
  title: string;
  description: string;
  status: PostStatus;
  body: string;
  coverUrl: string;
  coverAlt: string;
  // Frontmatter keys BurzBlog does not model, as YAML, kept on every round trip.
  extra: string;
  // Topics and categories in one list: lowercase words with hyphens.
  tags: string[];
  author: string;
  seoTitle: string;
  seoDescription: string;
  // An absolute https address, or empty.
  canonicalUrl: string;
  publishedOn: string | null;
  updatedOn: string | null;
  // When a draft publishes itself; null unless it is scheduled.
  publishAt: string | null;
  createdAt: string;
  updatedAt: string;
  updatedBy: string;
  version: number;
}

// The fields the editor can change; status changes go through post actions.
export type PostDraft = Pick<
  Post,
  | "slug"
  | "title"
  | "description"
  | "body"
  | "coverUrl"
  | "coverAlt"
  | "extra"
  | "tags"
  | "author"
  | "seoTitle"
  | "seoDescription"
  | "canonicalUrl"
  | "publishedOn"
  | "updatedOn"
>;

export interface PostSummary {
  slug: string;
  title: string;
  description: string;
  status: PostStatus;
  words: number;
  tags: string[];
  author: string;
  publishedOn: string | null;
  publishAt: string | null;
  updatedAt: string;
  updatedBy: string;
  version: number;
}

export interface TagCount {
  tag: string;
  count: number;
}

// Scheduled drafts are counted in draft as well.
export interface PostList {
  items: PostSummary[];
  // Posts matching the filter, of which items holds the first page(s).
  total: number;
  counts: Record<PostStatus | "scheduled", number>;
}

export interface Session {
  authenticated: boolean;
  username?: string;
  csrf?: string;
}

// The answer to a correct password when the account also needs a code.
export interface SecondFactorReply {
  second_factor: true;
  challenge: string;
}

export interface TwoFactorStatus {
  enabled: boolean;
  pending: boolean;
  recoveryCodes: number;
}

export interface Revision {
  version: number;
  title: string;
  status: PostStatus;
  savedAt: string;
  savedBy: string;
  post?: Post;
}

// What the revision list reports per version, enough to say what a save changed.
export interface RevisionState {
  slug: string;
  title: string;
  status: PostStatus;
  words: number;
  descriptionDigest: string;
  coverDigest: string;
  settingsDigest: string;
}

export interface RevisionSummary extends RevisionState {
  version: number;
  savedAt: string;
  savedBy: string;
}

export interface RevisionList {
  items: RevisionSummary[];
  current: RevisionState;
}

export interface Token {
  id: number;
  name: string;
  scope: "write" | "export";
  username: string;
  createdAt: string;
  lastUsedAt: string | null;
  // Only present in the response that creates the token.
  secret?: string;
}

export interface DeployStatus {
  configured: boolean;
  pending: boolean;
  lastAt: string | null;
  lastStatus: "" | "ok" | "failed";
  lastError: string;
}

export interface Settings {
  username: string;
  origin: string;
  version: string;
  deploy: DeployStatus;
}
