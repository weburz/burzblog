import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, errorMessage, rememberSession, request } from "../../app/utils/api";

afterEach(() => vi.unstubAllGlobals());
describe("authenticated requests", () => {
  it("fetches the CSRF token once, then sends it on every change", async () => {
    rememberSession({ authenticated: false });
    const fetch = vi
      .fn<typeof globalThis.fetch>()
      .mockResolvedValueOnce(
        Response.json({ authenticated: true, csrf: "session-bound" }),
      )
      .mockImplementation(() => Promise.resolve(Response.json({ saved: true })));
    vi.stubGlobal("fetch", fetch);
    await request("/api/posts/a", "PUT", { body: "x" });
    await request("/api/posts/a", "PUT", { body: "y" });
    expect(fetch).toHaveBeenCalledTimes(3);
    expect(fetch.mock.calls[1]?.[1]).toMatchObject({
      headers: { "X-CSRF-Token": "session-bound" },
      credentials: "same-origin",
    });
    expect(fetch.mock.calls[2]?.[1]).toMatchObject({
      headers: { "X-CSRF-Token": "session-bound" },
    });
  });
  it("sends a form as multipart, with the CSRF token and no JSON Content-Type", async () => {
    rememberSession({ authenticated: true, csrf: "token" });
    const fetch = vi
      .fn<typeof globalThis.fetch>()
      .mockResolvedValue(Response.json({ id: 1 }, { status: 201 }));
    vi.stubGlobal("fetch", fetch);
    const form = new FormData();
    form.append("file", new File(["x"], "a.png"));
    await request("/api/media", "POST", form);
    const init = fetch.mock.calls[0]?.[1];
    expect(init?.body).toBe(form);
    expect(init?.headers).toEqual({ "X-CSRF-Token": "token" });
  });
  it("retries once with a fresh token when the server rejects a stale one", async () => {
    rememberSession({ authenticated: true, csrf: "stale" });
    const fetch = vi
      .fn<typeof globalThis.fetch>()
      .mockResolvedValueOnce(
        Response.json({ error: { code: "csrf", message: "x" } }, { status: 403 }),
      )
      .mockResolvedValueOnce(Response.json({ authenticated: true, csrf: "fresh" }))
      .mockResolvedValueOnce(Response.json({ ok: true }));
    vi.stubGlobal("fetch", fetch);
    await expect(request("/api/posts", "POST", {})).resolves.toEqual({ ok: true });
    expect(fetch.mock.calls[2]?.[1]).toMatchObject({
      headers: { "X-CSRF-Token": "fresh" },
    });
  });
  it("reports an ended session so the page can ask to sign in again", async () => {
    const expired = vi.fn<() => void>();
    window.addEventListener("burzblog:session-expired", expired);
    vi.stubGlobal(
      "fetch",
      vi
        .fn<typeof globalThis.fetch>()
        .mockResolvedValue(
          Response.json(
            { error: { code: "authentication", message: "Sign in to continue." } },
            { status: 401 },
          ),
        ),
    );
    await expect(request("/api/posts")).rejects.toThrow("Sign in to continue.");
    expect(expired).toHaveBeenCalledOnce();
    window.removeEventListener("burzblog:session-expired", expired);
  });
  it("carries the server's version of a post on a save conflict", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof globalThis.fetch>().mockResolvedValue(
        Response.json(
          {
            error: { code: "conflict", message: "Changed." },
            current: { slug: "a", version: 4 },
          },
          { status: 409 },
        ),
      ),
    );
    const failure = (await request("/api/posts/a").catch((e: unknown) => e)) as ApiError;
    expect(failure.code).toBe("conflict");
    expect(failure.current?.version).toBe(4);
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
    await expect(
      request("/api/login", "POST", { username: "a", password: "x" }),
    ).rejects.toThrow("That password is not correct.");
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
    await expect(request("/api/posts")).rejects.toThrow("Nothing was saved");
  });
});

describe("two-factor answers", () => {
  it("does not treat a wrong code as an ended session", async () => {
    const expired = vi.fn<() => void>();
    window.addEventListener("burzblog:session-expired", expired);
    vi.stubGlobal(
      "fetch",
      vi
        .fn<typeof globalThis.fetch>()
        .mockResolvedValue(
          Response.json(
            { error: { code: "code", message: "That code is not right." } },
            { status: 401 },
          ),
        ),
    );
    rememberSession({ authenticated: false });
    await expect(
      request("/api/login/totp", "POST", { challenge: "c", code: "1" }),
    ).rejects.toMatchObject({ code: "code" });
    expect(expired).not.toHaveBeenCalled();
    window.removeEventListener("burzblog:session-expired", expired);
  });
  it("does not look up a CSRF token before the second sign-in step", async () => {
    rememberSession({ authenticated: false });
    const fetch = vi
      .fn<typeof globalThis.fetch>()
      .mockResolvedValue(Response.json({ authenticated: true, csrf: "x" }));
    vi.stubGlobal("fetch", fetch);
    await request("/api/login/totp", "POST", { challenge: "c", code: "123456" });
    expect(fetch).toHaveBeenCalledTimes(1);
  });
});
