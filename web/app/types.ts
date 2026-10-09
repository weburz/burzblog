export interface Article {
  path: string;
  revision: string;
  markdown: string;
  title: string;
  status: string;
  // Why the frontmatter cannot be read; `markdown` is then the file's exact text.
  problem?: string | null;
}
export interface PostSummary {
  path: string;
  // Empty when the file has no title in its frontmatter.
  title: string;
  status: string;
  // ISO 8601 time the file last changed on disk.
  modified: string;
}
export interface PostList {
  items: PostSummary[];
  // Files matching the current filter and search.
  total: number;
  // Every Markdown file discovered under the content folder.
  files: number;
  // Markdown files with names BurzBlog cannot use, and folders it will not read.
  left_out?: { unsupported_names: number; unsafe_folders: number };
}
export interface Session {
  authenticated: boolean;
  csrf?: string;
}
export interface Repository {
  branch: string | null;
  detached: boolean;
  clean: boolean;
  branches: string[];
  changed_markdown: string[];
  // Changed Markdown whose names the content store refuses, so cannot be committed.
  unsupported_markdown?: string[];
  changes: { path: string; index: string; worktree: string }[];
  upstream: string | null;
  ahead: number | null;
  behind: number | null;
  operation: string | null;
}
export interface Remote {
  name: string;
  transport: "https" | "file";
  authenticated: boolean;
}
export interface GitState {
  repository: Repository | null;
  remote_configured: boolean;
  remote: Remote | null;
}
