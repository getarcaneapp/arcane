import { defineConfig, devices } from '@playwright/test';

const baseURL = process.env.BASE_URL || 'http://localhost:3000';

export default defineConfig({
  testDir: '.',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  globalSetup: './setup/global-setup',
  globalTeardown: './setup/global-teardown',
  reporter:
    process.env.CI ?
      [['html', { outputFolder: '.report' }], ['github']]
    : [['line'], ['html', { open: 'never', outputFolder: '.report' }]],
  use: {
    baseURL,
    trace: 'on-first-retry',
    video: 'retain-on-failure',
  },
  projects: [
    {
      name: 'setup',
      testMatch: /setup\/.*\.setup\.ts/,
    },
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'], storageState: '.auth/login.json' },
      dependencies: ['setup'],
      testMatch: /spec\/.*\.spec\.ts/,
    },
  ],
});
