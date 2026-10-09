import type { Session } from "../types";

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
    message: string,
    // The server's curated, content-free cause ("title must not be empty."), when known.
    public readonly detail = "",
  ) {
    super(message);
  }
}

const sessionExpired = () => {
  if (typeof window !== "undefined") {
    window.dispatchEvent(new CustomEvent("burzblog:session-expired"));
  }
};

export const request = async <T>(
  path: string,
  method = "GET",
  body?: unknown,
): Promise<T> => {
  const headers: Record<string, string> = {};
  if (method !== "GET") {
    headers["Content-Type"] = "application/json";
    if (path !== "/api/login") {
      const session = await request<Session>("/api/session");
      if (!session.authenticated || !session.csrf) {
        sessionExpired();
        throw new ApiError(
          401,
          "authentication",
          "Your session expired. Sign in again, then retry.",
        );
      }
      headers["X-CSRF-Token"] = session.csrf;
    }
  }
  let response: Response;
  try {
    response = await fetch(path, {
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
      "Cannot reach BurzBlog. Your changes are not saved. Keep this tab open and try again.",
    );
  }
  if (!response.ok) {
    const value = (await response.json().catch(() => null)) as {
      error?: { code?: string; message?: string; detail?: string };
    } | null;
    const code = value?.error?.code ?? "request";
    // A wrong password or a foreign origin is a 401 too, but not an expired session.
    if (response.status === 401 && !["credentials", "origin"].includes(code))
      sessionExpired();
    throw new ApiError(
      response.status,
      code,
      value?.error?.message ??
        "The request failed. Your changes may not be saved; review the current file before retrying.",
      value?.error?.detail ?? "",
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
    : "The operation failed. Keep unsaved writing and review the file before retrying.";
};
