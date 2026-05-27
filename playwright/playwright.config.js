// @ts-check
import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  testMatch: '**/*.playwright.test.js',
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  use: {
    baseURL: 'http://127.0.0.1:19999',
    headless: true,
    trace: 'on-first-retry',
  },
  webServer: {
    command: 'RUNALL_PLAYWRIGHT_SERVER=1 go test -timeout 0 -run TestPlaywrightUIServer ./src',
    cwd: '..',
    url: 'http://127.0.0.1:19999',
    reuseExistingServer: !process.env.CI,
    stdout: 'pipe',
    stderr: 'pipe',
  },
});
