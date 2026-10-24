import type { Post, Session } from "../types";

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
    message: string,
    // The server's specific cause ("title must not be empty."), when known.
    public readonly detail = "",
    // On a save conflict, the post as it is on the server now.
    public readonly current: Post | null = null,
  ) {
    super(message);
  }
}

// The CSRF token of the signed-in session. It lives only in memory: a reload
// asks the server again.
let csrf = "";
export const rememberSession = (session: Session) => {
  csrf = session.authenticated ? (session.csrf ?? "") : "";
};

const sessionExpired = () => {
  csrf = "";
  if (typeof window !== "undefined")
    window.dispatchEvent(new CustomEvent("burzblog:session-expired"));
};

const send = async (path: string, method: string, body: unknown): Promise<Response> => {
  const headers: Record<string, string> = {};
  if (body !== undefined) headers["Content-Type"] = "application/json";
  if (method !== "GET" && csrf) headers["X-CSRF-Token"] = csrf;
  try {
    return await fetch(path, {
      method,
      headers,
      credentials: "same-origin",
      cache: "no-store",
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    throw new ApiError(
      0,
      "network",
      "Cannot reach BurzBlog. Nothing was saved. Check your connection and try again.",
    );
  }
};

const refreshCsrf = async (): Promise<boolean> => {
  const response = await send("/api/session", "GET", undefined);
  if (!response.ok) return false;
  const session = (await response.json()) as Session;
  rememberSession(session);
  return Boolean(csrf);
};

export const request = async <T>(
  path: string,
  method = "GET",
  body?: unknown,
): Promise<T> => {
  if (method !== "GET" && !csrf && path !== "/api/login") await refreshCsrf();
  let response = await send(path, method, body);
  if (response.status === 403 && method !== "GET") {
    // A token rotated by another tab: fetch the current one and retry once.
    const value = (await response
      .clone()
      .json()
      .catch(() => null)) as {
      error?: { code?: string };
    } | null;
    if (value?.error?.code === "csrf" && (await refreshCsrf()))
      response = await send(path, method, body);
  }
  if (!response.ok) {
    const value = (await response.json().catch(() => null)) as {
      error?: { code?: string; message?: string; detail?: string };
      current?: Post;
    } | null;
    const code = value?.error?.code ?? "request";
    // A wrong password is a 401 too, but not an expired session.
    if (response.status === 401 && code !== "credentials") sessionExpired();
    throw new ApiError(
      response.status,
      code,
      value?.error?.message ??
        "The request failed. Your changes may not be saved; reload the post before trying again.",
      value?.error?.detail ?? "",
      value?.current ?? null,
    );
  }
  return response.json() as Promise<T>;
};

const sentence = (text: string) => text.charAt(0).toUpperCase() + text.slice(1);

// The specific cause when the server named one, otherwise its general guidance.
export const errorMessage = (error: unknown): string => {
  if (error instanceof ApiError && error.detail) return sentence(error.detail);
  return error instanceof Error
    ? error.message
    : "That did not work. Your writing is still here; try again.";
};
