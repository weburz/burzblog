export interface Article {
  path: string;
  revision: string;
  markdown: string;
  title: string;
  status: string;
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
