import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useGameStore } from './game'
import type { SessionState, GuessResult } from '../types/game'

const mockSession: SessionState = {
  session_id: 'test-session',
  puzzle_id: 1,
  date: 'October 17, 2023',
  remaining: ['A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M', 'N', 'O', 'P'],
  solved: [],
  mistakes_left: 4,
  max_mistakes: 4,
  status: 'playing',
}

beforeEach(() => {
  setActivePinia(createPinia())
  // Mock fetch globally
  globalThis.fetch = vi.fn().mockResolvedValue({
    ok: true,
    json: () => Promise.resolve(mockSession),
  }) as unknown as typeof fetch
})

describe('tile selection', () => {
  it('toggles tile on click', () => {
    const store = useGameStore()
    store.applyGuessResult  // ensure store is reactive
    // Manually set state
    store.remaining.push(...mockSession.remaining)
    store.status = 'playing' as const

    store.toggleTile('A')
    expect(store.selected).toContain('A')
    store.toggleTile('A')
    expect(store.selected).not.toContain('A')
  })

  it('enforces max 4 selected', () => {
    const store = useGameStore()
    store.status = 'playing' as const
    ;['A', 'B', 'C', 'D'].forEach(w => store.toggleTile(w))
    store.toggleTile('E')
    expect(store.selected).toHaveLength(4)
    expect(store.selected).not.toContain('E')
  })

  it('deselectAll clears selection', () => {
    const store = useGameStore()
    store.status = 'playing' as const
    ;['A', 'B'].forEach(w => store.toggleTile(w))
    store.deselectAll()
    expect(store.selected).toHaveLength(0)
  })
})

describe('canSubmit', () => {
  it('is false when fewer than 4 selected', () => {
    const store = useGameStore()
    store.status = 'playing' as const
    store.toggleTile('A')
    expect(store.canSubmit).toBe(false)
  })

  it('is true when exactly 4 selected and playing', () => {
    const store = useGameStore()
    store.status = 'playing' as const
    ;['A', 'B', 'C', 'D'].forEach(w => store.toggleTile(w))
    expect(store.canSubmit).toBe(true)
  })

  it('is false when game is not playing', () => {
    const store = useGameStore()
    store.status = 'won' as const
    ;['A', 'B', 'C', 'D'].forEach(w => store.toggleTile(w))
    expect(store.canSubmit).toBe(false)
  })
})

describe('applyGuessResult', () => {
  it('correct guess removes tiles and adds solved group', () => {
    const store = useGameStore()
    store.remaining.push(...['A', 'B', 'C', 'D', 'E'])
    store.status = 'playing' as const
    store.mistakesLeft = 4

    const result: GuessResult = {
      correct: true,
      category: { title: 'YELLOW', words: ['A', 'B', 'C', 'D'], difficulty: 0 },
      one_away: false,
      mistakes_left: 4,
      status: 'playing',
    }
    store.applyGuessResult(result)

    expect(store.solved).toHaveLength(1)
    expect(store.solved[0].title).toBe('YELLOW')
    expect(store.remaining).toEqual(['E'])
    expect(store.selected).toHaveLength(0)
  })

  it('wrong guess decrements mistakes', () => {
    const store = useGameStore()
    store.mistakesLeft = 4
    store.status = 'playing' as const

    const result: GuessResult = {
      correct: false,
      one_away: false,
      mistakes_left: 3,
      status: 'playing',
    }
    store.applyGuessResult(result)

    expect(store.mistakesLeft).toBe(3)
    expect(store.solved).toHaveLength(0)
  })

  it('one_away sets toast message', () => {
    const store = useGameStore()
    store.mistakesLeft = 3
    store.status = 'playing' as const

    const result: GuessResult = {
      correct: false,
      one_away: true,
      mistakes_left: 3,
      status: 'playing',
    }
    store.applyGuessResult(result)
    expect(store.toast).toBe('One away!')
  })

  it('won status updates store', () => {
    const store = useGameStore()
    store.status = 'playing' as const

    const result: GuessResult = {
      correct: true,
      category: { title: 'LAST', words: ['M', 'N', 'O', 'P'], difficulty: 3 },
      one_away: false,
      mistakes_left: 4,
      status: 'won',
    }
    store.applyGuessResult(result)
    expect(store.status).toBe('won')
  })
})
