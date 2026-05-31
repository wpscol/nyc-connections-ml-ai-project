export interface PuzzleInfo {
  id: number
  date: string
  words: string[]
}

export interface SolvedGroup {
  title: string
  words: string[]
  difficulty: number // 0=yellow,1=green,2=blue,3=purple
}

export type GuessSource = 'player' | 'mcp' | 'api'

export interface GuessAttempt {
  words: string[]
  correct: boolean
  one_away: boolean
  difficulty: number // matched group difficulty, -1 if wrong
  source: GuessSource
  at: number // unix milliseconds
}

export interface GameStats {
  total_guesses: number
  correct_guesses: number
  groups_solved: number
  mistakes: number
  max_mistakes: number
  won: boolean
  accuracy: number
}

export interface GuessResult {
  correct: boolean
  category?: SolvedGroup
  one_away: boolean
  mistakes_left: number
  status: 'playing' | 'won' | 'lost'
  guessed?: string[]
  source?: GuessSource
}

export interface SessionState {
  session_id: string
  puzzle_id: number
  date: string
  remaining: string[]
  solved: SolvedGroup[]
  mistakes_left: number
  max_mistakes: number
  status: 'playing' | 'won' | 'lost'
  guesses?: GuessAttempt[]
  stats?: GameStats
}

export type WSEvent =
  | { type: 'guess_result'; payload: GuessResult }
  | { type: 'game_complete'; payload: { won: boolean; stats?: GameStats } }
  | { type: 'session_reset'; payload: SessionState }
  | { type: 'state_sync'; payload: { session_id: string } }
  | { type: 'config_update'; payload: { max_mistakes: number } }

export const DIFFICULTY_COLORS: Record<number, { bg: string; text: string }> = {
  0: { bg: 'bg-yellow-300',  text: 'text-yellow-900' },
  1: { bg: 'bg-green-500',   text: 'text-white' },
  2: { bg: 'bg-blue-500',    text: 'text-white' },
  3: { bg: 'bg-purple-600',  text: 'text-white' },
}

// Solid color values (for attempt pips drawn outside Tailwind class scanning)
export const DIFFICULTY_HEX: Record<number, string> = {
  0: '#fde047',
  1: '#22c55e',
  2: '#3b82f6',
  3: '#9333ea',
}

export const SOURCE_LABEL: Record<GuessSource, string> = {
  player: '🧑 You',
  mcp: '🤖 AI (MCP)',
  api: '🔌 API',
}
