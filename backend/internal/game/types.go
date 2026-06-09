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
	PuzzleID       int64          `json:"puzzle_id"`
	MistakesLeft   int            `json:"mistakes_left"`
	MaxMistakes    int            `json:"max_mistakes"`
	Solved         []SolvedGroup  `json:"solved"`
	RemainingWords []string       `json:"remaining_words"`
	Status         string         `json:"status"` // "playing","won","lost"
	Guesses        []GuessAttempt `json:"guesses"`
	// PriorGuesses accumulates all guesses from previous restarts of the same puzzle.
	// Never wiped on restart so the model can see what already worked/failed.
	PriorGuesses   []GuessAttempt `json:"prior_guesses,omitempty"`
}

// GuessAttempt records a single guess for the live attempt log.
type GuessAttempt struct {
	Words      []string `json:"words"`
	Correct    bool     `json:"correct"`
	OneAway    bool     `json:"one_away"`
	Difficulty int      `json:"difficulty"` // matched group difficulty, -1 if wrong
	Source     string   `json:"source"`     // "player","mcp","api"
	At         int64    `json:"at"`         // unix milliseconds
}

// GameStats summarizes how an attempt went, shown on completion.
type GameStats struct {
	TotalGuesses   int  `json:"total_guesses"`
	CorrectGuesses int  `json:"correct_guesses"`
	GroupsSolved   int  `json:"groups_solved"`
	Mistakes       int  `json:"mistakes"`
	MaxMistakes    int  `json:"max_mistakes"`
	Won            bool `json:"won"`
	Accuracy       int  `json:"accuracy"` // percent of correct guesses
}

type GuessResult struct {
	Correct      bool         `json:"correct"`
	Category     *SolvedGroup `json:"category,omitempty"`
	OneAway      bool         `json:"one_away"`
	MistakesLeft int          `json:"mistakes_left"`
	Status       string       `json:"status"`
	Guessed      []string     `json:"guessed,omitempty"` // submitted words, for WS animation
	Source       string       `json:"source,omitempty"`  // who guessed: player/mcp/api
}

// WSEvent is broadcast over WebSocket to all session clients.
type WSEvent struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}
