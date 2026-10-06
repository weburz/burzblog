import type { Session } from "../types";

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
    message: string,
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
          "Your session expired. Use Sign in again above, then retry. Your writing is still here.",
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
      error?: { code?: string; message?: string };
    } | null;
    if (response.status === 401) sessionExpired();
    throw new ApiError(
      response.status,
      value?.error?.code ?? "request",
      value?.error?.message ??
        "The request failed. Your changes may not be saved; review the current file before retrying.",
    );
  }
  return response.json() as Promise<T>;
};

export const errorMessage = (error: unknown): string =>
  error instanceof Error
    ? error.message
    : "The operation failed. Keep unsaved writing and review the file before retrying.";
