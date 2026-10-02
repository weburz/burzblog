export interface Article {
  path: string;
  revision: string;
  markdown: string;
  title: string;
  status: string;
}
export interface PostSummary {
  path: string;
  title: string;
  status: string;
}
export interface PostList {
  items: PostSummary[];
  total: number;
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
export interface GitState {
  repository: Repository | null;
  remote_configured: boolean;
}
