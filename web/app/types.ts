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
  publishedOn: string | null;
  updatedOn: string | null;
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
  | "publishedOn"
  | "updatedOn"
>;

export interface PostSummary {
  slug: string;
  title: string;
  description: string;
  status: PostStatus;
  words: number;
  publishedOn: string | null;
  updatedAt: string;
  updatedBy: string;
  version: number;
}

export interface PostList {
  items: PostSummary[];
  counts: Record<PostStatus, number>;
}

export interface Session {
  authenticated: boolean;
  username?: string;
  csrf?: string;
}

export interface Revision {
  version: number;
  title: string;
  status: PostStatus;
  savedAt: string;
  savedBy: string;
  post?: Post;
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
