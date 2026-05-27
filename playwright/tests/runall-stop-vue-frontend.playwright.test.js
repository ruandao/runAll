// @ts-check
import { test, expect } from '@playwright/test';

async function resetPlaywrightFixture(page) {
  const resp = await page.request.post('/api/test/reset-fixture');
  expect(resp.ok()).toBeTruthy();
}

test('关闭 vue-frontend 不出现 Failed to fetch', async ({ page }) => {
  const dialogs = [];
  page.on('dialog', async (dialog) => {
    dialogs.push(dialog.message());
    await dialog.dismiss();
  });

  await page.goto('/');
  await resetPlaywrightFixture(page);
  await page.reload();

  const serviceRow = page.locator('.service').filter({
    has: page.locator('.name', { hasText: /^vue-frontend$/ }),
  });
  await expect(serviceRow).toBeVisible();

  await serviceRow.getByRole('button', { name: '关闭' }).click();

  await expect.poll(async () => {
    return (await serviceRow.locator('.status').textContent())?.trim();
  }, { timeout: 10000 }).toBe('stopped');

  expect(dialogs.some((message) => message.includes('Failed to fetch'))).toBe(false);
});
