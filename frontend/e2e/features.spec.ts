import { test, expect } from '@playwright/test'
import { execFile } from 'node:child_process'
import { promisify } from 'node:util'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const execFileAsync = promisify(execFile)
const here = path.dirname(fileURLToPath(import.meta.url))

// Capture the session id + remaining words from the POST /api/session response
function trackSession(page: import('@playwright/test').Page) {
  const state = { sessionId: '', remaining: [] as string[] }
  page.on('response', async (response) => {
    if (
      response.url().includes('/api/session') &&
      response.request().method() === 'POST' &&
      !response.url().match(/\/(guess|restart|next|prev)$/)
    ) {
      try {
        const body = await response.json()
        state.sessionId = body.session_id
        state.remaining = body.remaining ?? []
      } catch { /* ignore */ }
    }
  })
  return state
}

async function apiGuess(page: import('@playwright/test').Page, sid: string, words: string[]) {
  return page.evaluate(
    async ({ sid, words }: { sid: string; words: string[] }) => {
      const res = await fetch(`/api/session/${sid}/guess`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ words }),
      })
      return res.json()
    },
    { sid, words }
  )
}

test('attempt log shows each API guess live with source badge', async ({ page }) => {
  const s = trackSession(page)
  await page.goto('/')
  await expect(page.locator('[data-testid="tile"]')).toHaveCount(16, { timeout: 15_000 })

  // First API guess (4 distinct words) — arrives over WS, no reload
  await apiGuess(page, s.sessionId, s.remaining.slice(0, 4))
  await expect(page.locator('[data-testid="attempt-row"]')).toHaveCount(1, { timeout: 5_000 })

  // Source badge should mark it as an API guess
  await expect(page.locator('[data-testid="attempt-row"] [data-source="api"]').first()).toBeVisible()

  // A second guess appends another row live
  await apiGuess(page, s.sessionId, s.remaining.slice(4, 8))
  await expect(page.locator('[data-testid="attempt-row"]')).toHaveCount(2, { timeout: 5_000 })
})

test('restart resets the board live via WebSocket', async ({ page }) => {
  const s = trackSession(page)
  await page.goto('/')
  await expect(page.locator('[data-testid="tile"]')).toHaveCount(16, { timeout: 15_000 })

  await apiGuess(page, s.sessionId, s.remaining.slice(0, 4))
  await expect(page.locator('[data-testid="attempt-row"]')).toHaveCount(1, { timeout: 5_000 })

  // External restart call — UI must react over WS without reload
  await page.evaluate(async (sid: string) => {
    await fetch(`/api/session/${sid}/restart`, { method: 'POST' })
  }, s.sessionId)

  // Board returns to 16 tiles and the attempts log clears
  await expect(page.locator('[data-testid="tile"]')).toHaveCount(16, { timeout: 5_000 })
  await expect(page.locator('[data-testid="attempt-row"]')).toHaveCount(0, { timeout: 5_000 })
})

test('next advances to a different puzzle live via WebSocket', async ({ page }) => {
  const s = trackSession(page)
  await page.goto('/')
  await expect(page.locator('[data-testid="tile"]')).toHaveCount(16, { timeout: 15_000 })

  const dateLocator = page.locator('[data-testid="puzzle-date"]')
  const beforeDate = (await dateLocator.textContent())?.trim()

  await page.evaluate(async (sid: string) => {
    await fetch(`/api/session/${sid}/next`, { method: 'POST' })
  }, s.sessionId)

  // Header date changes and the board is a fresh 16 tiles
  await expect
    .poll(async () => (await dateLocator.textContent())?.trim(), { timeout: 5_000 })
    .not.toBe(beforeDate)
  await expect(page.locator('[data-testid="tile"]')).toHaveCount(16, { timeout: 5_000 })
})

test('previous navigates back to a different puzzle live via WebSocket', async ({ page }) => {
  const s = trackSession(page)
  await page.goto('/')
  await expect(page.locator('[data-testid="tile"]')).toHaveCount(16, { timeout: 15_000 })

  const dateLocator = page.locator('[data-testid="puzzle-date"]')
  const beforeDate = (await dateLocator.textContent())?.trim()

  await page.evaluate(async (sid: string) => {
    await fetch(`/api/session/${sid}/prev`, { method: 'POST' })
  }, s.sessionId)

  await expect
    .poll(async () => (await dateLocator.textContent())?.trim(), { timeout: 5_000 })
    .not.toBe(beforeDate)
  await expect(page.locator('[data-testid="tile"]')).toHaveCount(16, { timeout: 5_000 })
})

test('prev then next returns to the original puzzle', async ({ page }) => {
  const s = trackSession(page)
  await page.goto('/')
  await expect(page.locator('[data-testid="tile"]')).toHaveCount(16, { timeout: 15_000 })

  const dateLocator = page.locator('[data-testid="puzzle-date"]')
  const start = (await dateLocator.textContent())?.trim()

  await page.evaluate(async (sid: string) => {
    await fetch(`/api/session/${sid}/prev`, { method: 'POST' })
  }, s.sessionId)
  await expect.poll(async () => (await dateLocator.textContent())?.trim(), { timeout: 5_000 }).not.toBe(start)

  await page.evaluate(async (sid: string) => {
    await fetch(`/api/session/${sid}/next`, { method: 'POST' })
  }, s.sessionId)
  await expect.poll(async () => (await dateLocator.textContent())?.trim(), { timeout: 5_000 }).toBe(start)
})

test('solving via MCP shows the stats panel with full results', async ({ page }) => {
  await page.goto('/')
  await expect(page.locator('[data-testid="tile"]')).toHaveCount(16, { timeout: 15_000 })

  // Run the MCP demo binary — it targets this page's WS-active session
  // and solves all 4 groups quickly.
  const backendDir = path.resolve(here, '../../backend')
  await execFileAsync('/tmp/mcpdemo', {
    cwd: backendDir,
    env: { ...process.env, STEP_DELAY_MS: '150' },
  })

  // The stats overlay should appear, driven entirely by WS events
  const panel = page.locator('[data-testid="stats-panel"]')
  await expect(panel).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('[data-testid="stats-result"]')).toContainText('Solved')
  await expect(page.locator('[data-testid="stat-groups"]')).toHaveText('4/4')
  await expect(page.locator('[data-testid="btn-next"]')).toBeVisible()
  await expect(page.locator('[data-testid="btn-restart"]')).toBeVisible()

  // Screenshot the stats panel as evidence
  await page.screenshot({ path: 'test-results/stats-panel.png' })
})
