import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { SolvedGroup, GuessResult, SessionState } from '../types/game'

export const useGameStore = defineStore('game', () => {
  const sessionId = ref<string>('')
  const puzzleDate = ref<string>('')
  const remaining = ref<string[]>([])
  const solved = ref<SolvedGroup[]>([])
  const selected = ref<string[]>([])
  const mistakesLeft = ref(4)
  const maxMistakes = ref(4)
  const status = ref<'idle' | 'playing' | 'won' | 'lost'>('idle')
  const shakingTiles = ref<string[]>([])
  const guessingTiles = ref<string[]>([]) // tiles AI is currently "showing" before result
  const toast = ref<string>('')
  const submitting = ref(false)

  // Tracks how many in-flight local REST guesses we should skip from WS echo
  let skipNextWSGuess = 0

  const canSubmit = computed(() => selected.value.length === 4 && !submitting.value && status.value === 'playing')

  async function init() {
    const res = await fetch('/api/session', { method: 'POST' })
    if (!res.ok) throw new Error('Failed to create session')
    const data: SessionState = await res.json()
    applySessionState(data)
  }

  function applySessionState(data: SessionState) {
    sessionId.value = data.session_id
    puzzleDate.value = data.date ?? puzzleDate.value
    remaining.value = data.remaining
    solved.value = data.solved ?? []
    mistakesLeft.value = data.mistakes_left
    maxMistakes.value = data.max_mistakes
    status.value = data.status === 'playing' ? 'playing' : data.status
    selected.value = []
    guessingTiles.value = []
  }

  function toggleTile(word: string) {
    if (status.value !== 'playing') return
    const idx = selected.value.indexOf(word)
    if (idx >= 0) {
      selected.value.splice(idx, 1)
    } else if (selected.value.length < 4) {
      selected.value.push(word)
    }
  }

  function deselectAll() {
    selected.value = []
  }

  function shuffle() {
    const arr = [...remaining.value]
    for (let i = arr.length - 1; i > 0; i--) {
      const j = Math.floor(Math.random() * (i + 1))
      ;[arr[i], arr[j]] = [arr[j], arr[i]]
    }
    remaining.value = arr
  }

  async function submitGuess() {
    if (!canSubmit.value) return
    submitting.value = true
    // Tell WS handler to absorb our own echo for this guess
    skipNextWSGuess++
    try {
      const res = await fetch(`/api/session/${sessionId.value}/guess`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ words: selected.value }),
      })
      if (!res.ok) throw new Error('Request failed')
      const result: GuessResult = await res.json()
      applyGuessResult(result, false)
    } catch {
      skipNextWSGuess = Math.max(0, skipNextWSGuess - 1)
    } finally {
      submitting.value = false
    }
  }

  // showAnimation = true when called from WS (AI/external guess), false for local user guess
  function applyGuessResult(result: GuessResult, showAnimation: boolean) {
    guessingTiles.value = []
    selected.value = []
    mistakesLeft.value = result.mistakes_left
    status.value = result.status === 'playing' ? 'playing' : result.status

    if (result.correct && result.category) {
      if (!solved.value.some(s => s.title === result.category!.title)) {
        solved.value.push(result.category)
      }
      remaining.value = remaining.value.filter(w => !result.category!.words.includes(w))
      showToast('')
    } else {
      if (result.one_away) showToast('One away!')
      const toShake = showAnimation && result.guessed?.length
        ? result.guessed
        : remaining.value.slice(0, 4)
      triggerShake(toShake)
    }
  }

  function triggerShake(words: string[]) {
    shakingTiles.value = [...words]
    setTimeout(() => { shakingTiles.value = [] }, 500)
  }

  function showToast(msg: string) {
    toast.value = msg
    if (msg) setTimeout(() => { toast.value = '' }, 2000)
  }

  // Called by WebSocket composable when MCP or external API drives updates
  function handleWSEvent(type: string, payload: unknown) {
    if (type === 'guess_result') {
      // Absorb echo of our own REST guess
      if (skipNextWSGuess > 0) {
        skipNextWSGuess--
        return
      }
      const result = payload as GuessResult
      const words = result.guessed ?? []
      if (words.length === 4) {
        // Briefly show which tiles the AI is guessing, then apply the result
        guessingTiles.value = words
        setTimeout(() => applyGuessResult(result, true), 700)
      } else {
        applyGuessResult(result, true)
      }
    } else if (type === 'game_complete') {
      const p = payload as { won: boolean }
      status.value = p.won ? 'won' : 'lost'
    } else if (type === 'state_sync') {
      if (sessionId.value) {
        fetch(`/api/session/${sessionId.value}`)
          .then(r => r.json())
          .then((s: SessionState) => applySessionState(s))
      }
    }
  }

  return {
    sessionId, puzzleDate, remaining, solved, selected,
    mistakesLeft, maxMistakes, status, shakingTiles, guessingTiles, toast,
    submitting, canSubmit,
    init, toggleTile, deselectAll, shuffle, submitGuess,
    applyGuessResult, handleWSEvent, showToast,
  }
})
