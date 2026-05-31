import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { SolvedGroup, GuessResult, SessionState, GuessAttempt, GameStats, SessionInfo } from '../types/game'

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
  const guessingTiles = ref<string[]>([]) // tiles an external client is "showing" before result
  const attempts = ref<GuessAttempt[]>([]) // live log of every try (player / AI / API)
  const stats = ref<GameStats | null>(null)
  const showStats = ref(false)
  const toast = ref<string>('')
  const submitting = ref(false)
  const sessions = ref<SessionInfo[]>([])

  // Tracks how many in-flight local REST guesses we should skip from WS echo
  let skipNextWSGuess = 0

  const canSubmit = computed(() => selected.value.length === 4 && !submitting.value && status.value === 'playing')
  const finished = computed(() => status.value === 'won' || status.value === 'lost')

  async function init() {
    const res = await fetch('/api/session', { method: 'POST' })
    if (!res.ok) throw new Error('Failed to create session')
    const data: SessionState = await res.json()
    applySessionState(data)
    await loadSessions()
  }

  async function newGame() {
    const res = await fetch('/api/session', { method: 'POST' })
    if (!res.ok) return
    applySessionState(await res.json())
    await loadSessions()
  }

  async function deleteSession(id: string) {
    await fetch(`/api/session/${id}`, { method: 'DELETE' })
    // If we just deleted the active session, start a new one
    if (id === sessionId.value) {
      const res = await fetch('/api/session', { method: 'POST' })
      if (res.ok) applySessionState(await res.json())
    }
    await loadSessions()
  }

  async function clearAllSessions() {
    await fetch('/api/sessions', { method: 'DELETE' })
    const res = await fetch('/api/session', { method: 'POST' })
    if (res.ok) applySessionState(await res.json())
    await loadSessions()
  }

  async function loadSessions() {
    try {
      const res = await fetch('/api/sessions')
      if (res.ok) sessions.value = await res.json()
    } catch { /* ignore */ }
  }

  async function switchSession(id: string) {
    const res = await fetch(`/api/session/${id}`)
    if (!res.ok) return
    applySessionState(await res.json())
    await loadSessions()
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
    shakingTiles.value = []
    attempts.value = data.guesses ?? []
    stats.value = data.stats ?? null
    showStats.value = false
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
    skipNextWSGuess++ // absorb our own WS echo for this guess
    try {
      const res = await fetch(`/api/session/${sessionId.value}/guess`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-Source': 'player' },
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

  async function restart() {
    if (!sessionId.value) return
    const res = await fetch(`/api/session/${sessionId.value}/restart`, { method: 'POST' })
    if (res.ok) { applySessionState(await res.json()); loadSessions() }
  }

  async function next() {
    if (!sessionId.value) return
    const res = await fetch(`/api/session/${sessionId.value}/next`, { method: 'POST' })
    if (res.ok) { applySessionState(await res.json()); loadSessions() }
  }

  async function prev() {
    if (!sessionId.value) return
    const res = await fetch(`/api/session/${sessionId.value}/prev`, { method: 'POST' })
    if (res.ok) { applySessionState(await res.json()); loadSessions() }
  }

  function recordAttempt(result: GuessResult) {
    attempts.value.push({
      words: result.guessed ?? [],
      correct: result.correct,
      one_away: result.one_away,
      difficulty: result.correct && result.category ? result.category.difficulty : -1,
      source: result.source ?? 'api',
      at: Date.now(),
    })
  }

  // showAnimation = true when called from WS (external guess), false for local user guess
  function applyGuessResult(result: GuessResult, showAnimation = false) {
    guessingTiles.value = []
    selected.value = []
    mistakesLeft.value = result.mistakes_left
    status.value = result.status === 'playing' ? 'playing' : result.status
    recordAttempt(result)

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
      if (skipNextWSGuess > 0) { // absorb echo of our own REST guess
        skipNextWSGuess--
        return
      }
      const result = payload as GuessResult
      const words = result.guessed ?? []
      if (words.length === 4 && result.status !== undefined) {
        // Briefly show which tiles the external client is guessing, then apply
        guessingTiles.value = words
        setTimeout(() => applyGuessResult(result, true), 700)
      } else {
        applyGuessResult(result, true)
      }
    } else if (type === 'game_complete') {
      const p = payload as { won: boolean; stats?: GameStats }
      status.value = p.won ? 'won' : 'lost'
      if (p.stats) stats.value = p.stats
      // Let the final tile/group animation finish before revealing stats
      setTimeout(() => { showStats.value = true }, 900)
    } else if (type === 'session_reset') {
      applySessionState(payload as SessionState)
    } else if (type === 'state_sync') {
      if (sessionId.value) {
        fetch(`/api/session/${sessionId.value}`)
          .then(r => r.json())
          .then((s: SessionState) => applySessionState(s))
      }
    } else if (type === 'config_update') {
      const p = payload as { max_mistakes: number }
      maxMistakes.value = p.max_mistakes
    }
  }

  return {
    sessionId, puzzleDate, remaining, solved, selected,
    mistakesLeft, maxMistakes, status, shakingTiles, guessingTiles,
    attempts, stats, showStats, toast, submitting, sessions, canSubmit, finished,
    init, newGame, toggleTile, deselectAll, shuffle, submitGuess, restart, next, prev,
    applyGuessResult, handleWSEvent, showToast, loadSessions, switchSession, deleteSession, clearAllSessions,
  }
})
