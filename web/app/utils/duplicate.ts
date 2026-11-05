import type { Post } from "../types";
import { request } from "./api";

// Asks the server for a draft copy of a saved post. The editor and the posts
// list both call it; the copy is returned so the caller can open it.
export const duplicatePost = (slug: string): Promise<Post> =>
  request<Post>(`/api/posts/${encodeURIComponent(slug)}/duplicate`, "POST");
