import { test, expect } from '@playwright/test'

/**
 * Verifies that when a guess is submitted directly via the REST API
 * (simulating an AI/MCP client), the browser UI updates in real time
 * through the WebSocket connection — no user interaction required.
 */

test('REST API guess updates UI via WebSocket without user interaction', async ({ page }) => {
  let sessionId = ''
  let remaining: string[] = []

  // Intercept the session creation response to capture session state
  page.on('response', async (response) => {
    if (
      response.url().includes('/api/session') &&
      response.request().method() === 'POST' &&
      !response.url().includes('/guess')
    ) {
      try {
        const body = await response.json()
        sessionId = body.session_id
        remaining = body.remaining ?? []
      } catch { /* ignore */ }
    }
  })

  await page.goto('/')

  // Wait for full 16-tile board to appear
  await expect(page.locator('[data-testid="tile"]')).toHaveCount(16, { timeout: 15_000 })
  expect(sessionId).toBeTruthy()
  expect(remaining).toHaveLength(16)

  // Capture initial mistake dot count
  const initialFilled = await page.locator('[data-testid="mistake-dot"][data-filled="true"]').count()
  expect(initialFilled).toBeGreaterThan(0)

  // === Submit guess via REST API — no browser click, simulating MCP ===
  await page.evaluate(
    async ({ sid, words }: { sid: string; words: string[] }) => {
      await fetch(`/api/session/${sid}/guess`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ words }),
      })
    },
    { sid: sessionId, words: remaining.slice(0, 4) }
  )

  // UI must update automatically via WebSocket — either:
  //   a) A correct guess: tile count drops from 16 → 12, solved group appears
  //   b) An incorrect guess: filled mistake dot count decreases
  await page.waitForFunction(
    ({ initial }: { initial: number }) => {
      const tiles = document.querySelectorAll('[data-testid="tile"]')
      const filled = document.querySelectorAll('[data-testid="mistake-dot"][data-filled="true"]')
      return tiles.length < 16 || filled.length < initial
    },
    { initial: initialFilled },
    { timeout: 5_000 }
  )
})

test('guessing tiles highlight amber when AI submits via WS', async ({ page }) => {
  let sessionId = ''
  let remaining: string[] = []

  page.on('response', async (response) => {
    if (
      response.url().includes('/api/session') &&
      response.request().method() === 'POST' &&
      !response.url().includes('/guess')
    ) {
      try {
        const body = await response.json()
        sessionId = body.session_id
        remaining = body.remaining ?? []
      } catch { /* ignore */ }
    }
  })

  await page.goto('/')
  await expect(page.locator('[data-testid="tile"]')).toHaveCount(16, { timeout: 15_000 })

  // Fire API guess without awaiting so we can observe the amber highlight
  page.evaluate(
    async ({ sid, words }: { sid: string; words: string[] }) => {
      await fetch(`/api/session/${sid}/guess`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ words }),
      })
    },
    { sid: sessionId, words: remaining.slice(0, 4) }
  )

  // During the 700ms animation window the tiles should turn amber
  // Wait for at least one amber tile to appear
  await expect(page.locator('.bg-amber-500')).toHaveCount(4, { timeout: 2_000 })

  // Then the animation ends and UI settles (tiles gone or mistake consumed)
  await page.waitForFunction(
    () => document.querySelectorAll('[data-testid="tile"]').length < 16 ||
          document.querySelectorAll('[data-testid="mistake-dot"][data-filled="true"]').length < 4,
    null,
    { timeout: 5_000 }
  )
})

test('MCP submit_guess via API call triggers solved group in UI', async ({ page }) => {
  let sessionId = ''
  let remaining: string[] = []

  page.on('response', async (response) => {
    if (
      response.url().includes('/api/session') &&
      response.request().method() === 'POST' &&
      !response.url().includes('/guess')
    ) {
      try {
        const body = await response.json()
        sessionId = body.session_id
        remaining = body.remaining ?? []
      } catch { /* ignore */ }
    }
  })

  await page.goto('/')
  await expect(page.locator('[data-testid="tile"]')).toHaveCount(16, { timeout: 15_000 })
  const initialSolvedCount = await page.locator('[data-testid="solved-group"]').count()
  const initialFilled = await page.locator('[data-testid="mistake-dot"][data-filled="true"]').count()

  // Submit chunks of 4 via API — simulating MCP brute-forcing groups
  const words = [...remaining]
  let solved = false
  for (let start = 0; start < words.length && !solved; start += 4) {
    const chunk = words.slice(start, start + 4)
    if (chunk.length < 4) break
    const result = await page.evaluate(
      async ({ sid, w }: { sid: string; w: string[] }) => {
        const res = await fetch(`/api/session/${sid}/guess`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ words: w }),
        })
        return res.json()
      },
      { sid: sessionId, w: chunk }
    )
    if (result.correct) solved = true
  }

  if (solved) {
    // WS animation takes 700ms — wait for the solved group to appear
    await expect(page.locator('[data-testid="solved-group"]')).toHaveCount(
      initialSolvedCount + 1,
      { timeout: 5_000 }
    )
    await expect(page.locator('[data-testid="tile"]')).toHaveCount(12, { timeout: 3_000 })
  } else {
    // All guesses wrong — wait for WS updates to settle (each has 700ms animation delay)
    await page.waitForFunction(
      ({ initial }: { initial: number }) => {
        const filled = document.querySelectorAll('[data-testid="mistake-dot"][data-filled="true"]')
        return filled.length < initial
      },
      { initial: initialFilled },
      { timeout: 8_000 }
    )
  }
})
