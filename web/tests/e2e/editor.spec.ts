import { gunzipSync } from "node:zlib";
import { expect, test } from "@playwright/test";
import { apiAs, password, signIn, unique } from "./helpers";

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
  const title = `A quieter writing workflow ${info.project.name}`;
  const slug = `a-quieter-writing-workflow-${info.project.name}`;
  expect((await request.get("/api/posts")).status()).toBe(401);

  await signIn(page);
  // New post opens the editor at once, on an untitled draft, with the cursor
  // in the title.
  await page.getByRole("button", { name: "New post", exact: true }).first().click();
  await expect(page).toHaveURL(/\/posts\/untitled(-\d+)?$/);
  const body = page.getByLabel("Post body in Markdown");
  await expect(page.getByLabel("Title", { exact: true })).toBeFocused();
  await page.keyboard.type(title);
  await expect(page.getByLabel("Title", { exact: true })).toHaveValue(title);

  // A draft saves itself once typing stops; nobody presses Save. The server
  // trims the title, and the editor must not stay "unsaved" after. "Saved"
  // flashes right after each save, before the quiet "Saved just now".
  const autosaved = async () => {
    await expect(
      page.getByRole("status").filter({ hasText: "Unsaved changes" }),
    ).toBeVisible();
    await expect(
      page.getByRole("status").filter({ hasText: /^\s*Saved\s*$/ }),
    ).toBeVisible({
      timeout: 10_000,
    });
    await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
  };
  await page.getByLabel("Title", { exact: true }).fill(`${title} `);
  await body.fill("# Hello\n\nWritten in the browser.\n");
  await autosaved();
  // The slug followed the title.
  await expect(page).toHaveURL(new RegExp(`/posts/${slug}$`));
  await page.reload();
  await expect(body).toHaveValue("# Hello\n\nWritten in the browser.\n");
  await expect(page).toHaveTitle(`${title} · BurzBlog`);
  // A run of autosaves is one entry in History, not one per pause.
  await body.fill("# Hello\n\nWritten in the browser, again.\n");
  await autosaved();
  await body.fill("# Hello\n\nWritten in the browser, once more.\n");
  await autosaved();

  const token = await (
    await request.post("/api/auth/token", {
      data: { username: "e2e", password, name: "e2e terminal" },
    })
  ).json();
  const auth = { Authorization: `Bearer ${token.secret}` };
  const latest = await (
    await request.get(`/api/posts/${slug}`, { headers: auth })
  ).json();
  const history = await (
    await request.get(`/api/posts/${slug}/revisions`, { headers: auth })
  ).json();
  // Where the post began, then one entry for the whole session.
  expect(history.items.map((r: { version: number }) => r.version)).toEqual([
    latest.version - 1,
    1,
  ]);

  // Published posts save by hand, so the rest of the workflow is explicit.
  const live = await request.post(`/api/posts/${slug}/publish`, {
    headers: auth,
    data: { version: latest.version },
  });
  expect(live.status()).toBe(200);
  await page.reload();
  await expect(body).toHaveValue("# Hello\n\nWritten in the browser, once more.\n");

  // The terminal saves a newer version while the browser has unsaved edits.
  const current = await request.get(`/api/posts/${slug}/markdown`, { headers: auth });
  const doc = (await current.text()).replace(
    "Written in the browser, once more.",
    "Rewritten in vim.",
  );
  const put = await request.put(`/api/posts/${slug}/markdown`, {
    headers: { ...auth, "If-Match": current.headers().etag! },
    data: doc,
  });
  expect(put.status()).toBe(200);

  await body.fill("# Hello\n\nMy browser edit.\n");
  await expect(
    page.getByRole("status").filter({ hasText: "Unsaved changes" }),
  ).toBeVisible();
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
  await reauth.getByLabel("Password", { exact: true }).fill(password);
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

  // Saving a live post lands in what reader builds download.
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: /^\s*Saved/ })).toBeVisible();
  const exported = await request.get("/api/export.tar.gz", { headers: auth });
  expect(exported.status()).toBe(200);
  expect(gunzipSync(await exported.body()).toString("latin1")).toContain(`${slug}.md`);

  expect(violations).toEqual([]);
});

test("split preview follows the block under the caret", async ({
  page,
  request,
}, info) => {
  test.skip(info.project.name !== "desktop", "Split layout is for desktop.");
  const slug = `block-sync-${unique()}`;
  const sections = Array.from(
    { length: 40 },
    (_, i) =>
      `## Section ${i}\n\nParagraph for section ${i}.\n\n\`\`\`\ncode ${i}\n\`\`\`\n`,
  );
  await (await apiAs(request)).createPost(slug, "Block sync", sections.join("\n"));
  await signIn(page);
  await page.goto(`/posts/${slug}`);
  await page.getByRole("button", { name: "Split" }).click();
  const frame = page.frameLocator('iframe[title="Preview of the published post"]');
  await expect(frame.locator("article > [data-line]").first()).toBeAttached();

  const body = page.getByLabel("Post body in Markdown");
  await body.click();
  await page.keyboard.press("Control+End");
  await page.keyboard.type("x");
  await expect(frame.getByRole("heading", { name: "Section 39" })).toBeInViewport();

  await page.keyboard.press("Control+Home");
  await expect(frame.getByRole("heading", { name: "Section 0" })).toBeInViewport();
});

test("posts list filters, searches and is usable by keyboard", async ({ page }, info) => {
  test.skip(info.project.name !== "desktop", "Keyboard shortcuts are for desktop.");
  await signIn(page);
  await page.keyboard.press("n");
  await expect(page).toHaveURL(/\/posts\/untitled(-\d+)?$/);
  await expect(page.getByLabel("Title", { exact: true })).toBeFocused();
  await page.keyboard.type("Keyboard first");
  await page.getByLabel("Post body in Markdown").fill("Typed with the keyboard.\n");
  // The draft saves itself and its slug follows the title.
  await expect(page).toHaveURL(/\/posts\/keyboard-first$/, { timeout: 10_000 });
  await expect(page.getByRole("status").filter({ hasText: /^\s*Saved/ })).toBeVisible();
  await page.getByRole("link", { name: "All posts" }).click();
  await expect(page.locator(".post-row").first()).toBeVisible();
  await page.keyboard.press("/");
  await expect(page.getByLabel("Search posts")).toBeFocused();
  await page.keyboard.type("keyboard");
  await expect(page.locator(".post-row")).toHaveCount(1);
  await expect(page).toHaveURL(/q=keyboard/);
  await page.getByRole("button", { name: /^Published/ }).click();
  await expect(page.locator(".empty-title")).toHaveText(/No published posts match/);
  await page.getByRole("button", { name: "Show all posts" }).click();
  await expect(page.getByLabel("Search posts")).toHaveValue("");
});

test("Duplicate opens a draft copy of the post", async ({ page, request }, info) => {
  const api = await apiAs(request);
  const slug = `dup-${info.project.name}-${unique()}`;
  await api.createPost(slug, "Template post", "Reusable body.", true);

  await signIn(page);
  await page.goto(`/posts/${slug}`);
  await expect(page.getByLabel("Post body in Markdown")).toBeVisible();
  await page.getByRole("button", { name: "More actions" }).click();
  await page.getByRole("menuitem", { name: "Duplicate" }).click();

  await expect(page).toHaveURL(new RegExp(`/posts/${slug}-copy$`));
  await expect(page.getByLabel("Title", { exact: true })).toHaveValue(
    "Template post (copy)",
  );
  await expect(page.getByLabel("Post body in Markdown")).toHaveValue("Reusable body.");
  expect(await api.status(`${slug}-copy`)).toBe("draft");
  expect(await api.status(slug)).toBe("published");
});

test("a row menu publishes a draft without opening it", async ({
  page,
  request,
}, info) => {
  const api = await apiAs(request);
  const tag = unique();
  const slug = `row-${info.project.name}-${tag}`;
  await api.createPost(slug, `Row menu ${tag}`);

  await signIn(page);
  await page.goto(`/?q=${slug}`);
  await expect(page.locator(".post-row")).toHaveCount(1);
  await page.getByRole("button", { name: `Actions for Row menu ${tag}` }).click();
  await page.getByRole("menuitem", { name: "Publish" }).click();
  await expect.poll(() => api.status(slug)).toBe("published");
  await expect(page).toHaveURL(/\/\?q=/);
  await expect(page.locator(".post-item .post-status")).toContainText(/published/i);
  // Nothing may push the page wider than the screen, phone included.
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  expect(overflow).toBeLessThanOrEqual(0);
});

test("selected rows are changed together from the bulk bar", async ({
  page,
  request,
}, info) => {
  const api = await apiAs(request);
  const tag = unique();
  const first = `bulk-a-${info.project.name}-${tag}`;
  const second = `bulk-b-${info.project.name}-${tag}`;
  await api.createPost(first, `Bulk one ${tag}`);
  await api.createPost(second, `Bulk two ${tag}`);

  await signIn(page);
  await page.goto(`/?q=${tag}`);
  await expect(page.locator(".post-row")).toHaveCount(2);
  await page.getByRole("checkbox", { name: `Select Bulk one ${tag}` }).click();
  await page.getByRole("checkbox", { name: `Select Bulk two ${tag}` }).click();
  const bar = page.getByRole("group", { name: "Actions for the selected posts" });
  await expect(bar).toContainText("2 selected");
  await bar.getByRole("button", { name: "Publish" }).click();

  await expect.poll(() => api.status(first)).toBe("published");
  await expect.poll(() => api.status(second)).toBe("published");
  await expect(bar).toBeHidden();
  await expect(page.getByLabel("Search posts")).toHaveValue(tag);

  // Tagging goes through the normal save and keeps the posts as they were.
  await page.getByRole("checkbox", { name: "Select all posts shown" }).click();
  await bar.getByRole("button", { name: "Add tag" }).click();
  await page.getByLabel("Tag to add to the selected posts").fill("Bulk Tagged");
  await page.getByLabel("Tag to add to the selected posts").press("Enter");
  await expect(page.locator(".post-tags")).toHaveCount(2);
  await expect(page.locator(".post-tags").first()).toHaveText("#bulk-tagged");
  expect(await api.status(first)).toBe("published");
});

test("j, k, x and Escape drive the posts list", async ({ page, request }, info) => {
  test.skip(info.project.name !== "desktop", "Keyboard shortcuts are for desktop.");
  const api = await apiAs(request);
  const tag = unique();
  await api.createPost(`keys-a-${tag}`, `Keys one ${tag}`);
  await api.createPost(`keys-b-${tag}`, `Keys two ${tag}`);

  await signIn(page);
  await page.goto(`/?q=${tag}`);
  await expect(page.locator(".post-row")).toHaveCount(2);
  await page.keyboard.press("j");
  const rows = page.locator(".post-row");
  await expect(rows.nth(0)).toBeFocused();
  await page.keyboard.press("j");
  await expect(rows.nth(1)).toBeFocused();
  await page.keyboard.press("k");
  await expect(rows.nth(0)).toBeFocused();
  await page.keyboard.press("x");
  const box = page.getByRole("checkbox", { name: /^Select Keys/ }).first();
  await expect(box).toBeChecked();
  await page.keyboard.press("Escape");
  await expect(box).not.toBeChecked();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/\/posts\/keys-/);
});
