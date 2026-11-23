import { expect, test } from "@playwright/test";

test("setup accepts a fresh fragment in the same tab and clears it", async ({ page }) => {
  await page.route("**/api/setup", (route) => route.fulfill({ json: { needed: true } }));
  await page.goto("/setup");
  await expect(page.getByText("Open the setup link printed by")).toBeVisible();
  await page.evaluate(() => {
    window.location.hash = "token=fresh-link";
  });
  await expect(page.getByLabel("Username")).toBeVisible();
  await expect(page).toHaveURL(/\/setup$/);
});
