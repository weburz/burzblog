import { readFileSync } from "node:fs";
import { expect, test } from "@playwright/test";

const username = process.env.BURZBLOG_SMOKE_USER ?? "admin";
const password = process.env.BURZBLOG_SMOKE_PASSWORD ?? "";
const tokenFile = process.env.BURZBLOG_DEPLOY_TOKEN_FILE;

test("normal workflows through the HTTPS proxy keep cookies, CSRF and CSP intact", async ({
  page,
  context,
  request,
  baseURL,
}) => {
  expect(baseURL?.startsWith("https://")).toBe(true);
  const violations: string[] = [];
  page.on("console", (message) => {
    if (message.type() === "error" && /Content Security Policy/i.test(message.text())) {
      violations.push(message.text());
    }
  });
  await page.addInitScript(() => {
    document.addEventListener("securitypolicyviolation", (event) => {
      console.error(`Content Security Policy violation: ${event.violatedDirective}`);
    });
  });
  await page.goto("/");
  await page.getByLabel("Username").fill(username);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Posts", exact: true })).toBeVisible();

  // Over HTTPS the cookie carries the __Host- prefix, which pins it to this origin.
  const [session] = (await context.cookies()).filter(
    (cookie) => cookie.name === "__Host-burzblog",
  );
  expect(session).toMatchObject({
    secure: true,
    httpOnly: true,
    sameSite: "Strict",
    path: "/",
  });

  // New post opens the editor on an untitled draft with the cursor in the title.
  const title = `Proxied browser check ${Date.now()}`;
  const slug = title.toLowerCase().replaceAll(" ", "-");
  await page.getByRole("button", { name: "New post", exact: true }).first().click();
  await expect(page).toHaveURL(/\/posts\/untitled(-\d+)?$/);
  await expect(page.getByLabel("Title", { exact: true })).toBeFocused();
  await page.keyboard.type(title);
  const body = page.getByLabel("Post body in Markdown");
  await body.fill("# Proxied\n\nWritten through the proxy.\n");
  // A draft saves itself once typing stops; the slug follows the title.
  const saved = page.getByRole("status").filter({ hasText: /^\s*Saved\s*$/ });
  await expect(saved).toBeVisible({ timeout: 10_000 });
  await expect(page).toHaveURL(new RegExp(`/posts/${slug}$`));

  // Another client (a terminal token) saves the same post first; the browser's
  // next save must meet a conflict instead of overwriting it.
  const token = await (
    await request.post("/api/auth/token", {
      data: { username, password, name: "deploy spec" },
    })
  ).json();
  const auth = { Authorization: `Bearer ${token.secret}` };
  const current = await (
    await request.get(`/api/posts/${slug}`, { headers: auth })
  ).json();
  expect(current.body).toContain("Written through the proxy.");
  const put = await request.put(`/api/posts/${slug}`, {
    headers: auth,
    data: {
      version: current.version,
      slug,
      title,
      body: "# Proxied\n\nRewritten from the terminal.\n",
    },
  });
  expect(put.status()).toBe(200);

  await body.fill("# Proxied\n\nMy browser edit.\n");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  const conflict = page.getByRole("alert").filter({ hasText: "saved a newer version" });
  await expect(conflict).toBeVisible();
  await expect(body).toHaveValue("# Proxied\n\nMy browser edit.\n");
  await conflict.getByRole("button", { name: "Use theirs" }).click();
  await expect(body).toHaveValue("# Proxied\n\nRewritten from the terminal.\n");
  await expect(conflict).toHaveCount(0);

  await body.fill("# Preview through the proxy\n\n*Rendered* safely.\n");
  await expect(
    page
      .frameLocator('iframe[title="Preview of the published post"]')
      .getByRole("heading", { name: "Preview through the proxy" }),
  ).toBeVisible();
  await expect(saved).toBeVisible({ timeout: 10_000 });
  const stored = await (
    await request.get(`/api/posts/${slug}`, { headers: auth })
  ).json();
  expect(stored.body).toContain("*Rendered* safely.");

  // The session cookie is there, so only the missing CSRF token stops this write.
  const forged = await page.evaluate(async () => {
    const response = await fetch("/api/posts", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ title: "Forged", slug: "forged-without-csrf" }),
    });
    return response.status;
  });
  expect(forged).toBe(403);
  expect(
    (await request.get("/api/posts/forged-without-csrf", { headers: auth })).status(),
  ).toBe(404);

  // The panel keeps preferences in localStorage, never credentials, and the
  // session cookie is invisible to scripts.
  const storage = await page.evaluate(async () => ({
    local: JSON.stringify({ ...localStorage }),
    session: JSON.stringify({ ...sessionStorage }),
    databases: (await indexedDB.databases()).map((database) => database.name),
    cookies: document.cookie,
  }));
  expect(storage.databases).toEqual([]);
  expect(storage.cookies).toBe("");
  for (const secret of [password, session?.value ?? "", token.secret]) {
    expect(storage.local).not.toContain(secret);
    expect(storage.session).not.toContain(secret);
  }
  if (tokenFile) {
    const deployToken = readFileSync(tokenFile, "utf8").trim();
    expect(await page.content()).not.toContain(deployToken);
    expect(JSON.stringify(await context.storageState())).not.toContain(deployToken);
  }

  // The editor has its own bar; the account menu lives on the posts list.
  await page.getByRole("link", { name: "All posts" }).click();
  await page.getByRole("button", { name: /^Account:/ }).click();
  await page.getByRole("menuitem", { name: "Sign out" }).click();
  await expect(page.getByLabel("Password", { exact: true })).toBeVisible();
  expect(violations).toEqual([]);
});
