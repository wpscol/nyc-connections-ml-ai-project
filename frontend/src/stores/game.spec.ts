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

describe('attempts log', () => {
  it('records each guess with its source and difficulty', () => {
    const store = useGameStore()
    store.status = 'playing' as const
    store.remaining.push('A', 'B', 'C', 'D', 'E', 'F', 'G', 'H')

    store.applyGuessResult({
      correct: true,
      category: { title: 'YELLOW', words: ['A', 'B', 'C', 'D'], difficulty: 0 },
      one_away: false,
      mistakes_left: 4,
      status: 'playing',
      guessed: ['A', 'B', 'C', 'D'],
      source: 'mcp',
    })
    store.applyGuessResult({
      correct: false,
      one_away: true,
      mistakes_left: 3,
      status: 'playing',
      guessed: ['E', 'F', 'G', 'H'],
      source: 'player',
    })

    expect(store.attempts).toHaveLength(2)
    expect(store.attempts[0]).toMatchObject({ correct: true, difficulty: 0, source: 'mcp' })
    expect(store.attempts[1]).toMatchObject({ correct: false, one_away: true, difficulty: -1, source: 'player' })
  })

  it('defaults source to api when omitted', () => {
    const store = useGameStore()
    store.status = 'playing' as const
    store.applyGuessResult({
      correct: false, one_away: false, mistakes_left: 3, status: 'playing', guessed: ['A', 'B', 'C', 'D'],
    })
    expect(store.attempts[0].source).toBe('api')
  })
})

describe('handleWSEvent', () => {
  it('game_complete sets status and stats', () => {
    const store = useGameStore()
    store.status = 'playing' as const
    store.handleWSEvent('game_complete', {
      won: true,
      stats: {
        total_guesses: 4, correct_guesses: 4, groups_solved: 4,
        mistakes: 0, max_mistakes: 4, won: true, accuracy: 100,
      },
    })
    expect(store.status).toBe('won')
    expect(store.stats?.accuracy).toBe(100)
    expect(store.stats?.groups_solved).toBe(4)
  })

  it('session_reset replaces board and clears attempts/stats', () => {
    const store = useGameStore()
    // dirty state
    store.solved.push({ title: 'X', words: ['A'], difficulty: 0 })
    store.attempts.push({ words: ['A', 'B', 'C', 'D'], correct: false, one_away: false, difficulty: -1, source: 'api', at: 0 })
    store.stats = { total_guesses: 1, correct_guesses: 0, groups_solved: 1, mistakes: 1, max_mistakes: 4, won: false, accuracy: 0 }
    store.showStats = true

    store.handleWSEvent('session_reset', {
      session_id: 'test-session',
      puzzle_id: 5,
      date: 'June 16, 2023',
      remaining: ['Q', 'R', 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z', 'A', 'B', 'C', 'D', 'E', 'F'],
      solved: [],
      mistakes_left: 4,
      max_mistakes: 4,
      status: 'playing',
      guesses: [],
    })

    expect(store.remaining).toHaveLength(16)
    expect(store.solved).toHaveLength(0)
    expect(store.attempts).toHaveLength(0)
    expect(store.showStats).toBe(false)
    expect(store.puzzleDate).toBe('June 16, 2023')
    expect(store.status).toBe('playing')
  })
})
