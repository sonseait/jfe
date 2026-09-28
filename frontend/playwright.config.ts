import { defineConfig, devices } from '@playwright/test';
const port = Number(process.env.JFE_TEST_PORT || 13000);
export default defineConfig({
  testDir: './tests/native-e2e',
  fullyParallel: false,
  workers: 1,
  use: { baseURL: `http://localhost:${port}`, trace: 'retain-on-failure', actionTimeout: 10000 },
  webServer: {
    command: `pnpm dev --port ${port}`,
    url: `http://localhost:${port}`,
    reuseExistingServer: false,
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
    { name: 'mobile', use: { ...devices['iPhone 13'], defaultBrowserType: 'chromium' } },
  ],
});
