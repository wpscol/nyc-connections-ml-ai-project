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

export interface GuessResult {
  correct: boolean
  category?: SolvedGroup
  one_away: boolean
  mistakes_left: number
  status: 'playing' | 'won' | 'lost'
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
}

export type WSEvent =
  | { type: 'guess_result'; payload: GuessResult }
  | { type: 'game_complete'; payload: { won: boolean } }
  | { type: 'state_sync'; payload: { session_id: string } }

export const DIFFICULTY_COLORS: Record<number, { bg: string; text: string }> = {
  0: { bg: 'bg-yellow-300',  text: 'text-yellow-900' },
  1: { bg: 'bg-green-500',   text: 'text-white' },
  2: { bg: 'bg-blue-500',    text: 'text-white' },
  3: { bg: 'bg-purple-600',  text: 'text-white' },
}
