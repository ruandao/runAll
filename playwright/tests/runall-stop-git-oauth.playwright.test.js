// @ts-check
import { test, expect } from '@playwright/test';

async function resetPlaywrightFixture(page) {
  const resp = await page.request.post('/api/test/reset-fixture');
  expect(resp.ok()).toBeTruthy();
}

test('混合所有权下关闭 git-oauth 可停掉整条 platform 链', async ({ page }) => {
  const dialogs = [];
  page.on('dialog', async (dialog) => {
    dialogs.push(dialog.message());
    await dialog.dismiss();
  });

  await page.goto('/');
  await resetPlaywrightFixture(page);
  await page.reload();
  const gitOAuthRow = page.locator('.service').filter({
    has: page.locator('.name', { hasText: /^git-oauth$/ }),
  });
  await expect(gitOAuthRow).toBeVisible();
  await gitOAuthRow.getByRole('button', { name: '关闭' }).click();

  await expect.poll(async () => {
    return (await gitOAuthRow.locator('.status').textContent())?.trim();
  }, { timeout: 10000 }).toBe('stopped');

  for (const name of ['saas-backend', 'ai-provider', 'vue-frontend']) {
    const row = page.locator('.service').filter({
      has: page.locator('.name', { hasText: new RegExp(`^${name}$`) }),
    });
    await expect.poll(async () => {
      return (await row.locator('.status').textContent())?.trim();
    }, { timeout: 10000 }).toBe('stopped');
  }

  expect(dialogs.some((message) => /Stop failed|takeover|Failed to fetch/i.test(message))).toBe(false);
});
