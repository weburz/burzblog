import { readFileSync, writeFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { join } from "node:path";
import { expect, test } from "@playwright/test";

test("browser commit and remote updates preserve ordinary Git workflows", async ({
  page,
}, info) => {
  const root = info.config.metadata.fixtureRoot as string;
  const git = (directory: string, ...args: string[]) =>
    execFileSync("git", ["-C", directory, ...args], { encoding: "utf8" });
  // The fixture API pushes/fetches over authenticated HTTPS into this bare repository.
  const remote = join(root, "https/served/remote.git");
  const token = readFileSync(join(root, "secrets/token"), "utf8").trim();
  await page.goto("/");
  await page.getByLabel("Administrator password").fill("browser fixture password");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.getByRole("button", { name: "New post", exact: true }).click();
  await page.getByLabel("Title", { exact: true }).fill("Git interoperability");
  await page.getByLabel("Filename / slug").fill("git-flow");
  await page.getByRole("button", { name: "Create draft" }).click();
  const editor = page.getByLabel("Markdown source, including frontmatter");
  await editor.fill("browser Git content\n");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Saved ✓" })).toBeVisible();
  await page.getByRole("button", { name: "← Posts" }).click();
  await page.getByRole("button", { name: "Repository", exact: true }).click();
  await expect(
    page.getByText("Remote: origin · HTTPS with operator credentials"),
  ).toBeVisible();
  await page.getByLabel("posts/git-flow.md", { exact: true }).check();
  await page.getByLabel("Commit message").fill("Browser commit");
  await page.getByRole("button", { name: "Commit selected files" }).click();
  await expect(page.getByText("No local changes", { exact: true })).toBeVisible();
  const pushed = page.waitForResponse(
    (response) =>
      response.url().endsWith("/api/git") && response.request().method() === "POST",
  );
  await page.getByRole("button", { name: "Push commits" }).click();
  expect((await pushed).status()).toBe(200);
  expect(git(remote, "show", "main:content/posts/git-flow.md")).toBe(
    "browser Git content\n",
  );
  const other = join(root, "other");
  git(root, "clone", remote, other);
  git(other, "config", "user.name", "External fixture");
  git(other, "config", "user.email", "fixture@example.invalid");
  const external = join(other, "content/posts/git-flow.md");
  writeFileSync(external, "external Git content\n");
  git(other, "add", "content/posts/git-flow.md");
  git(other, "-c", "commit.gpgsign=false", "commit", "-m", "External");
  git(other, "push");
  const fetched = page.waitForResponse(
    (response) =>
      response.url().endsWith("/api/git") && response.request().method() === "POST",
  );
  await page.getByRole("button", { name: "Check for updates" }).click();
  expect((await fetched).status()).toBe(200);
  await expect(page.getByRole("button", { name: "Apply safe update" })).toBeEnabled();
  await page.getByRole("button", { name: "Apply safe update" }).click();
  await expect
    .poll(() => readFileSync(join(root, "repo/content/posts/git-flow.md"), "utf8"))
    .toBe("external Git content\n");
  await page.getByRole("button", { name: "Posts", exact: true }).click();
  await page.getByRole("button", { name: /git-flow/ }).click();
  await expect(editor).toHaveValue("external Git content\n");
  await editor.fill("local divergent version\n");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Saved ✓" })).toBeVisible();
  await page.getByRole("button", { name: "← Posts" }).click();
  await page.getByRole("button", { name: "Repository", exact: true }).click();
  await page.getByLabel("posts/git-flow.md", { exact: true }).check();
  await page.getByLabel("Commit message").fill("Local divergent change");
  await page.getByRole("button", { name: "Commit selected files" }).click();
  await expect(page.getByText("No local changes", { exact: true })).toBeVisible();
  writeFileSync(external, "remote divergent version\n");
  git(other, "add", "content/posts/git-flow.md");
  git(other, "-c", "commit.gpgsign=false", "commit", "-m", "Remote divergence");
  git(other, "push");
  const refreshed = page.waitForResponse(
    (response) =>
      response.url().endsWith("/api/git") && response.request().method() === "POST",
  );
  await page.getByRole("button", { name: "Check for updates" }).click();
  expect((await refreshed).status()).toBe(200);
  await page.getByRole("button", { name: "Apply safe update" }).click();
  await expect(page.getByRole("alert")).toContainText("Sync needs attention");
  expect(readFileSync(join(root, "repo/content/posts/git-flow.md"), "utf8")).toBe(
    "local divergent version\n",
  );
  expect(await page.content()).not.toContain(token);
  expect(git(remote, "show", "main:content/posts/git-flow.md")).toBe(
    "remote divergent version\n",
  );
});
