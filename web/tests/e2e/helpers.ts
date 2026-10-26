import { expect, type APIRequestContext, type Page } from "@playwright/test";

export const password = "browser fixture password";

export const signIn = async (page: Page) => {
  await page.goto("/");
  await page.getByLabel("Username").fill("e2e");
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Posts", exact: true })).toBeVisible();
};

// A unique suffix keeps tests independent of each other and of earlier runs.
export const unique = (): string => Math.random().toString(36).slice(2, 8);

// Posts are made through the API with a terminal token, like `burzblog` does.
export const apiAs = async (request: APIRequestContext) => {
  const token = await (
    await request.post("/api/auth/token", {
      data: { username: "e2e", password, name: "e2e helper" },
    })
  ).json();
  const headers = { Authorization: `Bearer ${token.secret}` };
  return {
    headers,
    createPost: async (
      slug: string,
      title: string,
      body = "",
      publish = false,
    ): Promise<{ version: number }> => {
      const created = await request.post("/api/posts", {
        headers,
        data: { slug, title, body },
      });
      expect(created.status()).toBe(201);
      let post = await created.json();
      if (publish) {
        const live = await request.post(`/api/posts/${slug}/publish`, {
          headers,
          data: { version: post.version },
        });
        expect(live.status()).toBe(200);
        post = await live.json();
      }
      return post;
    },
    status: async (slug: string): Promise<string> =>
      (await (await request.get(`/api/posts/${slug}`, { headers })).json()).status,
  };
};
