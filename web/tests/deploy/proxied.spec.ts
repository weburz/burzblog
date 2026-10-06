import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { expect, test } from "@playwright/test";

const password = process.env.BURZBLOG_SMOKE_PASSWORD ?? "";
const site = process.env.BURZBLOG_DEPLOY_CONTENT_ROOT;
const tokenFile = process.env.BURZBLOG_DEPLOY_TOKEN_FILE;

test("normal workflows through the HTTPS proxy keep cookies, CSRF and CSP intact", async ({
  page,
  context,
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
  await page.getByLabel("Administrator password").fill(password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Posts", exact: true })).toBeVisible();

  const [session] = (await context.cookies()).filter(
    (cookie) => cookie.name === "burzblog_session",
  );
  expect(session).toMatchObject({
    secure: true,
    httpOnly: true,
    sameSite: "Strict",
    path: "/api",
  });

  const slug = `browser-proxy-${Date.now()}`;
  await page.getByRole("button", { name: "New post", exact: true }).click();
  await page.getByLabel("Title", { exact: true }).fill("Proxied browser check");
  await page.getByLabel("Filename / slug").fill(slug);
  await page.getByRole("button", { name: "Create draft" }).click();
  const editor = page.getByLabel("Markdown source, including frontmatter");
  await expect(editor).toBeVisible();
  const created = await editor.inputValue();
  await editor.fill(`${created}\nWritten through the proxy.\n`);
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Saved ✓" })).toBeVisible();

  if (site) {
    const file = join(site, "posts", `${slug}.md`);
    expect(readFileSync(file, "utf8")).toContain("Written through the proxy.");
    writeFileSync(file, `${readFileSync(file, "utf8")}External host edit.\n`);
    await editor.fill("stale browser text\n");
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect(page.getByRole("alert")).toContainText("changed");
    expect(readFileSync(file, "utf8")).toContain("External host edit.");
    await page.getByRole("button", { name: "Check disk" }).click();
    await page.getByRole("button", { name: "Use disk version" }).first().click();
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "Use disk version" })
      .click();
    await expect(editor).toHaveValue(/External host edit\./);
  }

  await editor.fill("# Preview through the proxy\n\n*Rendered* safely.\n");
  await page.getByRole("button", { name: "Preview", exact: true }).click();
  await expect(
    page.frameLocator('iframe[title="Article preview"]').getByRole("heading"),
  ).toHaveText("Preview through the proxy");
  await page.getByRole("button", { name: "← Posts" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Leave" }).click();

  await page.getByRole("button", { name: "Repository", exact: true }).click();
  await expect(page.getByText(/Remote: origin · HTTPS/)).toBeVisible();
  const fetched = page.waitForResponse(
    (response) =>
      response.url().endsWith("/api/git") && response.request().method() === "POST",
  );
  await page.getByRole("button", { name: "Check for updates" }).click();
  expect((await fetched).status()).toBe(200);
  const pushed = page.waitForResponse(
    (response) =>
      response.url().endsWith("/api/git") && response.request().method() === "POST",
  );
  await page.getByRole("button", { name: "Push commits" }).click();
  expect((await pushed).status()).toBe(200);

  const forged = await page.evaluate(async () => {
    const response = await fetch("/api/posts", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ title: "Forged", slug: "forged-without-csrf" }),
    });
    return response.status;
  });
  expect(forged).toBe(401);

  const storage = await page.evaluate(async () => ({
    local: JSON.stringify({ ...localStorage }),
    session: JSON.stringify({ ...sessionStorage }),
    databases: (await indexedDB.databases()).map((database) => database.name),
    cookies: document.cookie,
  }));
  expect(storage).toEqual({ local: "{}", session: "{}", databases: [], cookies: "" });
  if (tokenFile) {
    const token = readFileSync(tokenFile, "utf8").trim();
    expect(await page.content()).not.toContain(token);
    expect(JSON.stringify(await context.storageState())).not.toContain(token);
  }

  await page.getByRole("button", { name: "Account" }).click();
  await page.getByRole("menuitem", { name: "Sign out" }).click();
  await expect(page.getByLabel("Administrator password")).toBeVisible();
  expect(violations).toEqual([]);
});
