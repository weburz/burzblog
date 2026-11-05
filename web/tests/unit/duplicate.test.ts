import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, rememberSession } from "../../app/utils/api";
import { duplicatePost } from "../../app/utils/duplicate";

afterEach(() => vi.unstubAllGlobals());

describe("duplicatePost", () => {
  it("posts to the duplicate endpoint with the slug escaped and returns the copy", async () => {
    rememberSession({ authenticated: true, csrf: "token" });
    const fetch = vi
      .fn<typeof globalThis.fetch>()
      .mockResolvedValue(Response.json({ slug: "a-copy" }, { status: 201 }));
    vi.stubGlobal("fetch", fetch);
    await expect(duplicatePost("a b")).resolves.toEqual({ slug: "a-copy" });
    expect(fetch.mock.calls[0]?.[0]).toBe("/api/posts/a%20b/duplicate");
    expect(fetch.mock.calls[0]?.[1]).toMatchObject({
      method: "POST",
      headers: { "X-CSRF-Token": "token" },
    });
  });
  it("rejects with the server's message when the post is gone", async () => {
    rememberSession({ authenticated: true, csrf: "token" });
    vi.stubGlobal(
      "fetch",
      vi
        .fn<typeof globalThis.fetch>()
        .mockResolvedValue(
          Response.json(
            { error: { code: "not_found", message: "That post does not exist." } },
            { status: 404 },
          ),
        ),
    );
    await expect(duplicatePost("gone")).rejects.toBeInstanceOf(ApiError);
  });
});
