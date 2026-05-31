import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  timeout: 30_000,
  // Serial: the MCP test targets the one WS-active session, so only one
  // browser page may be connected at a time.
  fullyParallel: false,
  workers: 1,
  use: {
    baseURL: 'http://localhost:5173',
  },
  webServer: [
    {
      command: 'cd ../backend && go run ./cmd/server',
      port: 8080,
      reuseExistingServer: true,
      timeout: 60_000,
    },
    {
      command: 'npm run dev',
      port: 5173,
      reuseExistingServer: true,
    },
  ],
})
