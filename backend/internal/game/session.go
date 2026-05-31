package game

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

func CreateSession(db *sql.DB, puzzle Puzzle, maxMistakes int) (string, *GameState, error) {
	id := uuid.New().String()
	state := &GameState{
		PuzzleID:       puzzle.ID,
		MistakesLeft:   maxMistakes,
		MaxMistakes:    maxMistakes,
		Solved:         []SolvedGroup{},
		RemainingWords: AllWords(puzzle),
		Status:         "playing",
	}
	data, err := json.Marshal(state)
	if err != nil {
		return "", nil, err
	}
	now := time.Now().Unix()
	_, err = db.Exec(
		`INSERT INTO sessions(id,puzzle_id,state,created_at,updated_at) VALUES(?,?,?,?,?)`,
		id, puzzle.ID, string(data), now, now,
	)
	if err != nil {
		return "", nil, fmt.Errorf("insert session: %w", err)
	}
	return id, state, nil
}

func GetSession(db *sql.DB, id string) (*GameState, error) {
	var stateJSON string
	err := db.QueryRow(`SELECT state FROM sessions WHERE id=?`, id).Scan(&stateJSON)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("session not found")
	}
	if err != nil {
		return nil, err
	}
	var state GameState
	if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func SaveSession(db *sql.DB, id string, state *GameState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = db.Exec(
		`UPDATE sessions SET state=?, updated_at=? WHERE id=?`,
		string(data), time.Now().Unix(), id,
	)
	return err
}

func ApplyGuess(state *GameState, result *GuessResult, cat *Category) {
	if !result.Correct {
		state.MistakesLeft--
		if state.MistakesLeft <= 0 {
			state.Status = "lost"
		}
		return
	}

	words := make([]string, len(cat.Cards))
	for i, c := range cat.Cards {
		words[i] = c.Content
	}
	state.Solved = append(state.Solved, SolvedGroup{
		Title:      cat.Title,
		Words:      words,
		Difficulty: cat.Difficulty,
	})
	state.RemainingWords = RemoveWords(state.RemainingWords, words)

	if len(state.Solved) == 4 {
		state.Status = "won"
	}
}
