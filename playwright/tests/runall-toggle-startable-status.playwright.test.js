// @ts-check
import { test, expect } from '@playwright/test';

async function resetPlaywrightFixture(page) {
  const resp = await page.request.post('/api/test/reset-fixture');
  expect(resp.ok()).toBeTruthy();
}

test('failed 等非运行态服务显示启动按钮', async ({ page }) => {
  await page.goto('/');
  await resetPlaywrightFixture(page);
  await page.request.post('/api/test/set-status', {
    data: { name: 'saas-backend', status: 'failed' },
  });
  await page.reload();

  const row = page.locator('.service').filter({
    has: page.locator('.name', { hasText: /^saas-backend$/ }),
  });
  await expect(row.locator('.status')).toHaveText('failed');
  await expect(row.getByRole('button', { name: '启动' })).toBeVisible();
  await expect(row.getByRole('button', { name: '关闭' })).toHaveCount(0);
});
