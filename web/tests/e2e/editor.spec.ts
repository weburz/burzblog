import { readFileSync, writeFileSync, statSync, existsSync } from "node:fs";
import { join } from "node:path";
import { expect, test } from "@playwright/test";

test("browser and external editor share ordinary Markdown without silent loss", async ({
  page,
  context,
  request,
}, info) => {
  const root = info.config.metadata.fixtureRoot as string;
  const slug = `article-${info.project.name}`;
  const file = join(root, "repo/content/posts", `${slug}.md`);
  const outbound: string[] = [];
  page.on("request", (outgoing) => {
    if (
      !outgoing.url().startsWith("http://127.0.0.1:3000") &&
      !outgoing.url().startsWith("data:")
    )
      outbound.push(outgoing.url());
  });
  expect((await request.get("/api/posts")).status()).toBe(401);
  await page.goto("/");
  await page.getByLabel("Administrator password").fill("browser fixture password");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Posts", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "New post", exact: true }).click();
  await page.getByLabel("Title", { exact: true }).fill("A quieter writing workflow");
  await page.getByLabel("Filename / slug").fill(slug);
  await page.getByRole("button", { name: "Create draft" }).click();
  const editor = page.getByLabel("Markdown source, including frontmatter");
  await expect(editor).toBeVisible();
  expect(readFileSync(file, "utf8")).toContain("status: draft");
  const original = readFileSync(file, "utf8");
  await editor.fill(
    `${original}\n# A quieter writing workflow\n\nWritten in the browser.\n`,
  );
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Saved ✓" })).toBeVisible();
  expect(readFileSync(file, "utf8")).toContain("Written in the browser.");
  const externallyEdited =
    "---\r\ntitle: 'External title' # keep\r\ncustom_field: retained\r\nstatus: draft\r\n---\r\nExternal body";
  writeFileSync(file, externallyEdited);
  await page.getByRole("button", { name: "Check disk" }).click();
  await expect(editor).toHaveValue(externallyEdited.replace(/\r\n/g, "\n"));
  const before = statSync(file);
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Saved ✓" })).toBeVisible();
  expect(readFileSync(file, "utf8")).toBe(externallyEdited);
  expect(statSync(file).mtimeMs).toBe(before.mtimeMs);
  await editor.fill("unsaved browser writing");
  await page.getByRole("button", { name: "Check disk" }).click();
  await expect(editor).toHaveValue("unsaved browser writing");
  writeFileSync(file, "external conflicting writing\n");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("changed");
  await expect(editor).toHaveValue("unsaved browser writing");
  expect(readFileSync(file, "utf8")).toBe("external conflicting writing\n");
  await page.getByRole("button", { name: "Check disk" }).click();
  await expect(page.getByText("This file changed outside BurzBlog.")).toBeVisible();
  page.once("dialog", (dialog) => dialog.accept());
  await page.getByRole("button", { name: "Use disk version" }).click();
  await expect(editor).toHaveValue("external conflicting writing\n");
  await context.setOffline(true);
  await editor.fill("keep this during network failure");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("not saved");
  await expect(editor).toHaveValue("keep this during network failure");
  await context.setOffline(false);
  await editor.fill(
    '# Safe preview\n<script>window.parent.xss=true</script>\n<img src=x onerror="window.parent.xss=true">\n[bad](javascript:alert(1))\n![pixel](https://tracker.invalid/pixel)',
  );
  await page.getByRole("button", { name: "Preview", exact: true }).click();
  const frame = page.frameLocator('iframe[title="Article preview"]');
  await expect(frame.getByRole("heading", { name: "Safe preview" })).toBeVisible();
  expect(await frame.locator("script, img, iframe").count()).toBe(0);
  expect(await page.evaluate(() => Object.hasOwn(window, "xss"))).toBe(false);
  await page.screenshot({ path: info.outputPath("editor.png"), fullPage: true });
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
  ).toBe(true);
  await page.getByRole("button", { name: "Markdown", exact: true }).click();
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Saved ✓" })).toBeVisible();
  await page.getByRole("button", { name: "Publish", exact: true }).click();
  await expect(page.getByText("Published", { exact: true })).toBeVisible();
  expect(readFileSync(file, "utf8")).toContain("status: published");
  await page.getByText("File options", { exact: true }).click();
  await page.getByLabel("Move / rename file").fill("../outside.md");
  await page.getByRole("button", { name: "Move file", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("cannot be performed safely");
  expect(existsSync(file)).toBe(true);
  expect(existsSync(join(root, "repo/outside.md"))).toBe(false);
  await page.getByLabel("Move / rename file").fill(`posts/${slug}-renamed.md`);
  await page.getByRole("button", { name: "Move file", exact: true }).click();
  await expect(page.getByText(`posts/${slug}-renamed.md`, { exact: true })).toBeVisible();
  expect(existsSync(file)).toBe(false);
  page.once("dialog", (dialog) => dialog.accept());
  await page.getByRole("button", { name: "Archive post" }).click();
  await expect(page.getByRole("heading", { name: "Posts", exact: true })).toBeVisible();
  expect(outbound).toEqual([]);
});
