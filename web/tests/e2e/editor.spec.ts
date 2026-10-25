import { gunzipSync } from "node:zlib";
import { expect, test, type Page } from "@playwright/test";

const password = "browser fixture password";

const signIn = async (page: Page) => {
  await page.goto("/");
  await page.getByLabel("Username").fill("e2e");
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Posts", exact: true })).toBeVisible();
};

test("writing in the browser and the terminal never loses work", async ({
  page,
  context,
  request,
}, info) => {
  // The production shell ships a strict CSP; any violation is a regression.
  const violations: string[] = [];
  page.on("console", (message) => {
    if (message.type() === "error" && /Content Security Policy/i.test(message.text()))
      violations.push(message.text());
  });
  const slug = `quiet-workflow-${info.project.name}`;
  expect((await request.get("/api/posts")).status()).toBe(401);

  await signIn(page);
  await page.getByRole("button", { name: "New post", exact: true }).first().click();
  const dialog = page.getByRole("dialog", { name: "New post" });
  await dialog.getByLabel("Title").fill("A quieter writing workflow");
  await dialog.getByLabel("Slug").fill(slug);
  await dialog.getByRole("button", { name: "Create draft" }).click();
  await expect(page).toHaveURL(new RegExp(`/posts/${slug}$`));
  const body = page.getByLabel("Post body in Markdown");
  await expect(page.getByLabel("Title", { exact: true })).toHaveValue(
    "A quieter writing workflow",
  );

  // The server trims the title; the editor must not stay "unsaved" after.
  await page.getByLabel("Title", { exact: true }).fill("A quieter writing workflow ");
  await body.fill("# Hello\n\nWritten in the browser.\n");
  await expect(
    page.getByRole("status").filter({ hasText: "Unsaved changes" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: /^\s*Saved/ })).toBeVisible();
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
  await page.reload();
  await expect(body).toHaveValue("# Hello\n\nWritten in the browser.\n");
  await expect(page).toHaveTitle("A quieter writing workflow · BurzBlog");

  // The terminal saves a newer version while the browser has unsaved edits.
  const token = await (
    await request.post("/api/auth/token", {
      data: { username: "e2e", password, name: "e2e terminal" },
    })
  ).json();
  const auth = { Authorization: `Bearer ${token.secret}` };
  const current = await request.get(`/api/posts/${slug}/markdown`, { headers: auth });
  const doc = (await current.text()).replace(
    "Written in the browser.",
    "Rewritten in vim.",
  );
  const put = await request.put(`/api/posts/${slug}/markdown`, {
    headers: { ...auth, "If-Match": current.headers().etag! },
    data: doc,
  });
  expect(put.status()).toBe(200);

  await body.fill("# Hello\n\nMy browser edit.\n");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  const conflict = page.getByRole("alert").filter({ hasText: "saved a newer version" });
  await expect(conflict).toBeVisible();
  await expect(body).toHaveValue("# Hello\n\nMy browser edit.\n");
  await conflict.getByRole("button", { name: "Compare" }).click();
  await expect(conflict.getByRole("region", { name: "Differences" })).toContainText(
    "Rewritten in vim.",
  );
  await conflict.getByRole("button", { name: "Use theirs" }).click();
  await expect(body).toHaveValue("# Hello\n\nRewritten in vim.\n");
  await expect(conflict).toHaveCount(0);

  // Leaving with unsaved writing asks first; the writing stays.
  await body.fill("# Hello\n\nUnsaved thought.\n");
  await page.getByRole("link", { name: "All posts" }).click();
  const leave = page.getByRole("dialog", { name: "Leave without saving?" });
  await expect(leave).toBeVisible();
  await leave.getByRole("button", { name: "Cancel" }).click();
  await expect(leave).toHaveCount(0);
  await expect(body).toHaveValue("# Hello\n\nUnsaved thought.\n");

  // Another tab finds the unsaved writing kept in this browser, and the
  // backup survives a visit that does not restore it.
  await page.waitForTimeout(800);
  const glance = await context.newPage();
  await glance.goto(`/posts/${slug}`);
  await expect(
    glance.getByRole("alert").filter({ hasText: "still in this browser" }),
  ).toBeVisible();
  await glance.waitForTimeout(1000);
  await glance.close();
  const second = await context.newPage();
  await second.goto(`/posts/${slug}`);
  const recovered = second
    .getByRole("alert")
    .filter({ hasText: "still in this browser" });
  await expect(recovered).toBeVisible();
  await recovered.getByRole("button", { name: "Bring them back" }).click();
  await expect(second.getByLabel("Post body in Markdown")).toHaveValue(
    "# Hello\n\nUnsaved thought.\n",
  );
  await second.close();

  // An ended session keeps the writing and resumes after signing in.
  await context.clearCookies();
  await page.getByRole("button", { name: "Save", exact: true }).click();
  const reauth = page.getByRole("dialog", { name: "Your session ended" });
  await expect(reauth).toBeVisible();
  await reauth.getByLabel("Password").fill(password);
  await reauth.getByRole("button", { name: "Sign in" }).click();
  await expect(reauth).toBeHidden();
  await expect(body).toHaveValue("# Hello\n\nUnsaved thought.\n");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: /^\s*Saved/ })).toBeVisible();

  // The preview never runs scripts from a post.
  await body.fill(
    '# Safe preview\n\n<script>window.parent.xss=true</script>\n<img src=x onerror="window.parent.xss=true">\n\n[bad](javascript:alert(1))\n',
  );
  // Narrow screens show one pane at a time.
  const narrow = (await page.getByRole("button", { name: "Split" }).count()) === 0;
  if (narrow) await page.getByRole("button", { name: "Preview" }).click();
  const frame = page.frameLocator('iframe[title="Preview of the published post"]');
  await expect(frame.getByRole("heading", { name: "Safe preview" })).toBeVisible();
  expect(
    await frame.locator("article script, article img, a[href^=javascript]").count(),
  ).toBe(0);
  expect(await page.evaluate(() => Object.hasOwn(window, "xss"))).toBe(false);
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
  ).toBe(true);
  await page.screenshot({ path: info.outputPath("editor.png") });
  if (narrow) await page.getByRole("button", { name: "Write" }).click();

  // Publishing saves first, then reader builds can download it.
  await page.getByRole("button", { name: "Publish", exact: true }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Save and publish" })
    .click();
  await expect(page.getByRole("button", { name: "Publish", exact: true })).toHaveCount(0);
  const published = await request.get(`/api/posts/${slug}`, { headers: auth });
  expect((await published.json()).status).toBe("published");
  const exported = await request.get("/api/export.tar.gz", { headers: auth });
  expect(exported.status()).toBe(200);
  expect(gunzipSync(await exported.body()).toString("latin1")).toContain(`${slug}.md`);

  expect(violations).toEqual([]);
});

test("posts list filters, searches and is usable by keyboard", async ({ page }, info) => {
  test.skip(info.project.name !== "desktop", "Keyboard shortcuts are for desktop.");
  await signIn(page);
  await page.keyboard.press("n");
  const dialog = page.getByRole("dialog", { name: "New post" });
  await dialog.getByLabel("Title").fill("Keyboard first");
  await expect(dialog.getByLabel("Slug")).toHaveValue("keyboard-first");
  await dialog.getByLabel("Title").press("Enter");
  await expect(page).toHaveURL(/\/posts\/keyboard-first$/);
  await page.getByRole("link", { name: "All posts" }).click();
  await expect(page.locator(".post-row").first()).toBeVisible();
  await page.keyboard.press("/");
  await expect(page.getByLabel("Search posts")).toBeFocused();
  await page.keyboard.type("keyboard");
  await expect(page.locator(".post-row")).toHaveCount(1);
  await expect(page).toHaveURL(/q=keyboard/);
  await page.getByRole("button", { name: /^Published/ }).click();
  await expect(page.getByText("No published posts match")).toBeVisible();
  await page.getByRole("button", { name: "Show all posts" }).click();
  await expect(page.getByLabel("Search posts")).toHaveValue("");
});
