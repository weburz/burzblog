import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, errorMessage, request } from "../../app/utils/api";

afterEach(() => vi.unstubAllGlobals());
describe("authenticated requests", () => {
  it("gets a fresh CSRF token without browser persistence", async () => {
    const fetch = vi
      .fn<typeof globalThis.fetch>()
      .mockResolvedValueOnce(
        Response.json({ authenticated: true, csrf: "session-bound" }),
      )
      .mockResolvedValueOnce(Response.json({ saved: true }));
    vi.stubGlobal("fetch", fetch);
    await request("/api/post", "PUT", { markdown: "body" });
    expect(fetch.mock.calls[1]?.[1]).toMatchObject({
      headers: { "X-CSRF-Token": "session-bound" },
      credentials: "same-origin",
    });
  });
  it("does not submit an expired-session mutation", async () => {
    const fetch = vi
      .fn<typeof globalThis.fetch>()
      .mockResolvedValue(Response.json({ authenticated: false }));
    vi.stubGlobal("fetch", fetch);
    await expect(request("/api/post", "PUT", {})).rejects.toThrow("Your session expired");
    expect(fetch).toHaveBeenCalledTimes(1);
  });
  it("treats a wrong password as a wrong password, not an expired session", async () => {
    const expired = vi.fn<() => void>();
    window.addEventListener("burzblog:session-expired", expired);
    vi.stubGlobal(
      "fetch",
      vi
        .fn<typeof globalThis.fetch>()
        .mockResolvedValue(
          Response.json(
            { error: { code: "credentials", message: "That password is not correct." } },
            { status: 401 },
          ),
        ),
    );
    await expect(request("/api/login", "POST", { password: "x" })).rejects.toThrow(
      "That password is not correct.",
    );
    expect(expired).not.toHaveBeenCalled();
    window.removeEventListener("burzblog:session-expired", expired);
  });
  it("prefers the server's specific cause over its general guidance", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof globalThis.fetch>().mockResolvedValue(
        Response.json(
          {
            error: {
              code: "invalid",
              message: "The operation cannot be performed safely.",
              detail: "title must not be empty.",
            },
          },
          { status: 400 },
        ),
      ),
    );
    const failure = await request("/api/posts").catch((error: unknown) => error);
    expect(failure).toBeInstanceOf(ApiError);
    expect((failure as ApiError).detail).toBe("title must not be empty.");
    expect(errorMessage(failure)).toBe("Title must not be empty.");
    expect(errorMessage(new ApiError(400, "invalid", "General guidance."))).toBe(
      "General guidance.",
    );
  });
  it("does not report a failed network request as saved", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof globalThis.fetch>().mockRejectedValue(new Error("offline")),
    );
    await expect(request("/api/posts")).rejects.toThrow("not saved");
  });
});
