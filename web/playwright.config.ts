import { defineConfig, devices } from "@playwright/test";

/**
 * The end-to-end suite drives the real stack, so it needs Postgres, cmd/api and
 * cmd/worker already running (see the README). Playwright only starts the web
 * app, and reuses one that is already up.
 */
export default defineConfig({
  testDir: "./e2e",
  timeout: 60_000,
  expect: { timeout: 15_000 },
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: [["list"]],
  use: {
    ...devices["Desktop Chrome"],
    baseURL: process.env.E2E_BASE_URL ?? "http://localhost:3000",
    // The approved mockups are drawn at 1440×900, so the suite checks the app at
    // the size it was designed for.
    viewport: { width: 1440, height: 900 },
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  webServer: {
    command: "npm run start",
    url: "http://localhost:3000/login",
    reuseExistingServer: true,
    timeout: 120_000,
  },
});
