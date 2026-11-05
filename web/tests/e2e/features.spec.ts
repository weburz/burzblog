import { execFileSync } from "node:child_process";
import { join } from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { apiAs, signIn, unique } from "./helpers";

// These cover desktop controls (hover menus, shortcuts, the toolbar), so only
// the desktop project runs them; see playwright.config.ts.

const openPost = async (page: Page, slug: string) => {
  await page.goto(`/posts/${slug}`);
  await expect(page.getByLabel("Post body in Markdown")).toBeVisible();
};

test("dark mode is set in Settings, survives a reload and follows the account menu", async ({
  page,
}) => {
  await signIn(page);
  await page.getByRole("link", { name: "Settings", exact: true }).click();
  const dark = page.getByRole("radio", { name: "Dark" });
  await dark.click();
  await expect(dark).toHaveAttribute("aria-checked", "true");
  await expect(page.locator("html")).toHaveClass(/dark/);

  await page.reload();
  await expect(page.getByRole("radio", { name: "Dark" })).toHaveAttribute(
    "aria-checked",
    "true",
  );
  await expect(page.locator("html")).toHaveClass(/dark/);

  await page.getByRole("button", { name: "Account: e2e" }).click();
  await page.getByRole("menuitem", { name: "Theme" }).click();
  await page.getByRole("menuitemcheckbox", { name: "Light" }).click();
  await expect(page.locator("html")).not.toHaveClass(/dark/);
  await expect(page.getByRole("radio", { name: "Light" })).toHaveAttribute(
    "aria-checked",
    "true",
  );
});

test("the posts list sorts by title and keeps the sort in the address", async ({
  page,
  request,
}) => {
  const api = await apiAs(request);
  const tag = unique();
  await api.createPost(`sort-${tag}-b`, `Sortcheck ${tag} Bravo`);
  await api.createPost(`sort-${tag}-c`, `Sortcheck ${tag} Charlie`);
  await api.createPost(`sort-${tag}-a`, `Sortcheck ${tag} Alpha`);

  await signIn(page);
  await page.getByLabel("Search posts").fill(`sort-${tag}`);
  await expect(page.locator(".post-row")).toHaveCount(3);
  await page.getByRole("combobox", { name: "Sort posts" }).click();
  await page.getByRole("option", { name: "Title A–Z" }).click();
  const titles = page.locator(".post-row .post-title");
  const expected = ["Alpha", "Bravo", "Charlie"].map((n) => `Sortcheck ${tag} ${n}`);
  await expect(titles).toHaveText(expected);
  await expect(page).toHaveURL(/sort=title/);

  await page.reload();
  await expect(page).toHaveURL(/sort=title/);
  await expect(titles).toHaveText(expected);
  await expect(page.getByRole("combobox", { name: "Sort posts" })).toContainText(
    "Title A–Z",
  );
});

test("Undo after Unpublish puts the post back on the site", async ({ page, request }) => {
  const api = await apiAs(request);
  const slug = `undo-${unique()}`;
  await api.createPost(slug, "Undo me", "Body.", true);

  await signIn(page);
  await openPost(page, slug);
  await page.getByRole("button", { name: "More actions" }).click();
  await page.getByRole("menuitem", { name: "Unpublish" }).click();
  const toast = page.getByRole("listitem").filter({ hasText: "Unpublished" });
  await expect(toast).toBeVisible();
  await expect.poll(() => api.status(slug)).toBe("draft");

  await toast.getByRole("button", { name: "Undo" }).click();
  await expect.poll(() => api.status(slug)).toBe("published");
  await expect(page.getByRole("button", { name: "Publish", exact: true })).toHaveCount(0);
});

test("View on site explains the missing site URL for a published post", async ({
  page,
  request,
}) => {
  const api = await apiAs(request);
  const slug = `link-${unique()}`;
  await api.createPost(slug, "Link me", "Body.", true);

  await signIn(page);
  await openPost(page, slug);
  await page.getByRole("button", { name: "More actions" }).click();
  await page.getByRole("menuitem", { name: "View on site" }).click();
  await expect(
    page.getByLabel("Notifications (F8)").getByText("Set BURZBLOG_SITE_URL"),
  ).toBeVisible();
});

test("history summarises each save and marks the current version", async ({
  page,
  request,
}) => {
  const api = await apiAs(request);
  const slug = `history-${unique()}`;
  await api.createPost(slug, "History check", "one two three");
  const current = await request.get(`/api/posts/${slug}/markdown`, {
    headers: api.headers,
  });
  const saved = await request.put(`/api/posts/${slug}/markdown`, {
    headers: { ...api.headers, "If-Match": current.headers().etag! },
    data: (await current.text()).replace("one two three", "one two three four five"),
  });
  expect(saved.status()).toBe(200);

  await signIn(page);
  await openPost(page, slug);
  await page.getByRole("button", { name: "History", exact: true }).click();
  const panel = page.getByRole("dialog", { name: "History" });
  const row = panel.locator(".history-current");
  await expect(row).toContainText("Current version");
  await expect(row).toContainText("v2");
  await expect(row).toContainText("+2 words");
  await expect(panel.getByRole("button", { name: /^Version 1/ })).toContainText(
    "Created",
  );
});

test("Download all posts saves a dated backup archive", async ({ page }) => {
  await signIn(page);
  await page.getByRole("link", { name: "Settings", exact: true }).click();
  const [download] = await Promise.all([
    page.waitForEvent("download"),
    page.getByRole("link", { name: "Download all posts" }).click(),
  ]);
  expect(download.suggestedFilename()).toMatch(
    /^burzblog-backup-\d{4}-\d{2}-\d{2}\.tar\.gz$/,
  );
});

test("Settings warns until a full snapshot exists, then shows the newest", async ({
  page,
}) => {
  await signIn(page);
  await page.getByRole("link", { name: "Settings", exact: true }).click();
  const backup = page.getByRole("region", { name: "Backup" });
  await expect(backup).toContainText("No full snapshot yet");

  // The host runs the command against the server's data while it serves.
  const root = test.info().config.metadata.fixtureRoot as string;
  execFileSync(join(root, "burzblog"), [
    "snapshot",
    "create",
    join(root, "snapshots"),
    "--data",
    join(root, "data"),
  ]);
  await page.reload();
  await expect(backup).toContainText("Last full snapshot just now");
  await expect(backup).toContainText(/burzblog-snapshot-\d{8}T\d{6}\.\d{3}Z\.tar\.gz/);
});

test("the shortcuts switch turns off single-key shortcuts", async ({ page }) => {
  await signIn(page);
  await page.keyboard.press("/");
  await expect(page.getByLabel("Search posts")).toBeFocused();
  await page.getByLabel("Search posts").blur();

  await page.getByRole("link", { name: "Settings", exact: true }).click();
  const toggle = page.getByRole("switch", { name: "Keyboard shortcuts" });
  await expect(toggle).toBeChecked();
  await toggle.click();
  await expect(toggle).not.toBeChecked();

  await page.getByRole("link", { name: "Posts", exact: true }).click();
  await expect(page.getByLabel("Search posts")).toBeVisible();
  await page.keyboard.press("/");
  await expect(page.getByLabel("Search posts")).not.toBeFocused();
  await page.keyboard.press("n");
  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByRole("heading", { name: "Posts", exact: true })).toBeVisible();
});

test("the image dialog rejects unsafe addresses and inserts a valid image", async ({
  page,
  request,
}) => {
  const api = await apiAs(request);
  const slug = `image-${unique()}`;
  await api.createPost(slug, "Image check");

  await signIn(page);
  await openPost(page, slug);
  const body = page.getByLabel("Post body in Markdown");
  await page.getByRole("button", { name: "Image", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Insert image" });
  await dialog.getByLabel("Image address").fill("javascript:alert(1)");
  await dialog.getByRole("button", { name: "Insert image" }).click();
  await expect(dialog.getByRole("alert")).toContainText("starts with https://");
  await expect(body).toHaveValue("");

  await dialog.getByLabel("Image address").fill("https://example.com/photo.png");
  await dialog.getByLabel("Alt text").fill("A photo");
  await dialog.getByRole("button", { name: "Insert image" }).click();
  await expect(dialog).toHaveCount(0);
  await expect(body).toHaveValue("![A photo](https://example.com/photo.png)");
});

test("a file dropped on the body is uploaded and linked where the caret is", async ({
  page,
  request,
}) => {
  const api = await apiAs(request);
  const slug = `drop-${unique()}`;
  await api.createPost(slug, "Drop check", "Intro\n");

  await signIn(page);
  await openPost(page, slug);
  const body = page.getByLabel("Post body in Markdown");
  await body.focus();
  await page.keyboard.press("End");
  // A real 1×1 PNG, so the server accepts it; a text file is refused and
  // must leave nothing behind.
  const png =
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==";
  const dataTransfer = await page.evaluateHandle(
    ({ png: encoded }) => {
      const dt = new DataTransfer();
      const bytes = Uint8Array.from(atob(encoded), (c) => c.charCodeAt(0));
      dt.items.add(new File([bytes], "dot.png", { type: "image/png" }));
      dt.items.add(new File(["not an image"], "notes.txt", { type: "text/plain" }));
      return dt;
    },
    { png },
  );
  await body.dispatchEvent("drop", { dataTransfer });
  await expect(body).toHaveValue(
    /^Intro\n!\[dot\]\(http:\/\/[^)]+\/media\/[a-z0-9]+\.png\)$/,
    {
      timeout: 10_000,
    },
  );
  await expect(body).not.toHaveValue(/Uploading/);
  await expect(body).not.toHaveValue(/notes\.txt/);
  await expect(page.getByText("Could not upload").first()).toBeVisible();
  const url = (await body.inputValue()).match(/\(([^)]+)\)/)?.[1] ?? "";
  expect((await request.get(url)).status()).toBe(200);
});

test("focus mode toggles with Ctrl+Shift+F and leaves with Esc", async ({
  page,
  request,
}) => {
  const api = await apiAs(request);
  const slug = `focus-${unique()}`;
  await api.createPost(slug, "Focus check");

  await signIn(page);
  await openPost(page, slug);
  const shell = page.locator(".editor-shell");
  const leave = page.getByRole("button", { name: "Leave focus mode" });
  await expect(shell).toHaveAttribute("data-focus", "false");
  await page.getByLabel("Post body in Markdown").focus();
  await page.keyboard.press("Control+Shift+F");
  await expect(shell).toHaveAttribute("data-focus", "true");
  await expect(leave).toBeVisible();
  await expect(page.getByLabel("Post body in Markdown")).toBeFocused();

  await page.keyboard.press("Escape");
  await expect(shell).toHaveAttribute("data-focus", "false");
  await expect(leave).toHaveCount(0);

  await page.keyboard.press("Control+Shift+F");
  await expect(shell).toHaveAttribute("data-focus", "true");
  await page.keyboard.press("Control+Shift+F");
  await expect(shell).toHaveAttribute("data-focus", "false");
});

test("media: search by name, filter by type, and edit alt text in place", async ({
  page,
  request,
}) => {
  const api = await apiAs(request);
  const tag = unique();
  // A PDF with unique bytes, so the server stores a new record each run.
  const uploaded = await request.post("/api/media", {
    headers: api.headers,
    multipart: {
      file: {
        name: `menu-${tag}.pdf`,
        mimeType: "application/pdf",
        buffer: Buffer.from(
          `%PDF-1.4\n% ${tag}\n1 0 obj\n<<>>\nendobj\ntrailer\n<<>>\n%%EOF\n`,
        ),
      },
    },
  });
  expect(uploaded.status()).toBe(201);

  await signIn(page);
  await page.getByRole("link", { name: "Media", exact: true }).click();
  await page.getByLabel("Search files").fill(`MENU-${tag}`);
  const card = page.getByRole("listitem").filter({ hasText: `menu-${tag}.pdf` });
  await expect(card).toHaveCount(1);
  await expect(card.getByText("Not used in any post")).toBeVisible();

  await page.getByRole("button", { name: "Images", exact: true }).click();
  await expect(card).toHaveCount(0);
  await expect(page.getByText("No files match.")).toBeVisible();
  await page.getByRole("button", { name: "PDFs", exact: true }).click();
  await expect(card).toHaveCount(1);

  const alt = card.getByLabel(`Alt text for menu-${tag}.pdf`);
  await alt.fill("Lunch menu");
  await alt.press("Enter");
  await expect
    .poll(async () => {
      const list = await request.get(`/api/media?q=menu-${tag}`, {
        headers: api.headers,
      });
      return (await list.json()).items[0]?.alt;
    })
    .toBe("Lunch menu");

  // Escape puts the saved text back.
  await alt.fill("changed my mind");
  await alt.press("Escape");
  await expect(alt).toHaveValue("Lunch menu");

  // The alt text is searchable.
  await page.getByLabel("Search files").fill("lunch menu");
  await expect(card).toHaveCount(1);
});

test("the tags field suggests existing tags and still takes new ones", async ({
  page,
  request,
}) => {
  const api = await apiAs(request);
  const id = unique();
  const existing = `suggest-${id}-golang`;
  const other = await api.createPost(`tagged-${id}`, "Tagged elsewhere");
  const tagged = await request.put(`/api/posts/tagged-${id}`, {
    headers: api.headers,
    data: {
      version: other.version,
      slug: `tagged-${id}`,
      title: "Tagged elsewhere",
      tags: [existing],
    },
  });
  expect(tagged.status()).toBe(200);
  const slug = `suggest-${id}`;
  await api.createPost(slug, "Suggest tags");

  await signIn(page);
  await openPost(page, slug);
  await page.getByRole("button", { name: "Post settings" }).click();
  const dialog = page.getByRole("dialog");
  const tags = dialog.getByRole("combobox", { name: "Tags" });
  await tags.fill(`suggest-${id}-go`);
  await expect(
    page.getByRole("option", { name: new RegExp(`${existing}.*1 post`) }),
  ).toBeVisible();
  await page.getByRole("option", { name: new RegExp(existing) }).click();
  await expect(dialog.getByText(existing)).toBeVisible();

  // Text left in the field when the writer clicks elsewhere becomes a tag.
  await tags.fill("Left Alone");
  await dialog.getByRole("heading", { name: "Post settings" }).click();
  await expect(dialog.getByText("left-alone")).toBeVisible();
  await expect(tags).toHaveValue("");

  // Text no tag matches becomes a new tag, cleaned like before.
  await tags.fill("Brand New");
  await tags.press("Enter");
  await tags.fill("typed, ");
  await expect(dialog.getByText("brand-new")).toBeVisible();
  await expect(dialog.getByText("typed", { exact: true })).toBeVisible();

  // The draft saves itself once typing stops.
  await expect
    .poll(
      async () =>
        (await (await request.get(`/api/posts/${slug}`, { headers: api.headers })).json())
          .tags,
      { timeout: 15_000 },
    )
    .toEqual([existing, "left-alone", "brand-new", "typed"]);
});

test("a new post that is abandoned empty leaves no draft behind", async ({
  page,
  request,
}) => {
  const api = await apiAs(request);
  await signIn(page);
  const count = async () => {
    const list = await request.get("/api/posts?status=draft&limit=500", {
      headers: api.headers,
    });
    const body = (await list.json()) as { items: { slug: string }[] };
    return body.items.filter((post) => /^untitled(-\d+)?$/.test(post.slug)).length;
  };
  const before = await count();

  await page.getByRole("button", { name: "New post", exact: true }).first().click();
  await expect(page).toHaveURL(/\/posts\/untitled(-\d+)?$/);
  await expect(page.getByLabel("Title", { exact: true })).toBeFocused();
  expect(await count()).toBe(before + 1);

  await page.getByRole("link", { name: "All posts" }).click();
  await expect(page.getByRole("heading", { name: "Posts", exact: true })).toBeVisible();
  await expect.poll(count).toBe(before);
});

test("the list, editor and settings never scroll sideways, and selected ticks show", async ({
  page,
  request,
}) => {
  const api = await apiAs(request);
  const tag = unique();
  const slug = `wide-${tag}`;
  const title = `A very long title that keeps going ${tag} `.repeat(4).slice(0, 160);
  await api.createPost(slug, title);
  await signIn(page);
  await page.getByLabel("Search posts").fill(slug);
  await expect(page.locator(".post-row")).toHaveCount(1);
  await expect(page.locator(".post-title")).toHaveAttribute("title", title);

  const fits = () =>
    page.evaluate(
      () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
    );
  for (const width of [320, 390, 683, 1365]) {
    await page.setViewportSize({ width, height: 800 });
    await page.goto(`/?q=${slug}`);
    await expect(page.locator(".post-row")).toHaveCount(1);
    expect(await fits(), `/ at ${width}`).toBe(true);

    // The selected row's tick stays visible with the mouse somewhere else.
    await page
      .getByRole("checkbox", { name: `Select ${title}`, exact: true })
      .click({ force: true });
    await page.mouse.move(1, 1);
    await expect(page.locator(".post-item .post-select").first()).toHaveCSS(
      "opacity",
      "1",
    );
    await page.getByRole("button", { name: "Clear" }).click();

    await page.goto(`/posts/${slug}`);
    await expect(page.getByLabel("Post body in Markdown")).toBeVisible();
    expect(await fits(), `editor at ${width}`).toBe(true);
    await page.goto("/settings");
    await expect(
      page.getByRole("heading", { name: "Settings", exact: true }),
    ).toBeVisible();
    expect(await fits(), `/settings at ${width}`).toBe(true);
  }
});
