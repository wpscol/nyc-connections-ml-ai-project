package game

type Card struct {
	Content  string `json:"content"`
	Position int    `json:"position"`
}

type Category struct {
	Title      string `json:"title"`
	Difficulty int    `json:"difficulty"` // 0=yellow,1=green,2=blue,3=purple
	Cards      []Card `json:"cards"`
}

type Puzzle struct {
	ID         int64      `json:"id"`
	Date       string     `json:"date"`
	Categories []Category `json:"categories"`
}

type SolvedGroup struct {
	Title      string   `json:"title"`
	Words      []string `json:"words"`
	Difficulty int      `json:"difficulty"`
}

type GameState struct {
	PuzzleID       int64         `json:"puzzle_id"`
	MistakesLeft   int           `json:"mistakes_left"`
	MaxMistakes    int           `json:"max_mistakes"`
	Solved         []SolvedGroup `json:"solved"`
	RemainingWords []string      `json:"remaining_words"`
	Status         string        `json:"status"` // "playing","won","lost"
}

type GuessResult struct {
	Correct      bool         `json:"correct"`
	Category     *SolvedGroup `json:"category,omitempty"`
	OneAway      bool         `json:"one_away"`
	MistakesLeft int          `json:"mistakes_left"`
	Status       string       `json:"status"`
	Guessed      []string     `json:"guessed,omitempty"` // submitted words, for WS animation
}

// WSEvent is broadcast over WebSocket to all session clients.
type WSEvent struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}
