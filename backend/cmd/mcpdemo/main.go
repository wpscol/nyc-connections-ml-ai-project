// mcpdemo solves the WS-active session by reading puzzle answers directly
// from the local SQLite DB and submitting correct guesses via the REST API.
// Used by the e2e test suite to drive the stats-panel verification.
package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "modernc.org/sqlite"
)

const baseURL = "http://localhost:8080"

type sessionInfo struct {
	ID       string `json:"session_id"`
	Status   string `json:"status"`
	WSActive bool   `json:"ws_active"`
}

type sessionState struct {
	SessionID string `json:"session_id"`
	PuzzleID  int64  `json:"puzzle_id"`
	Status    string `json:"status"`
}

type category struct {
	Title      string `json:"title"`
	Difficulty int    `json:"difficulty"`
	Cards      []struct {
		Content  string `json:"content"`
		Position int    `json:"position"`
	} `json:"cards"`
}

func main() {
	delay := 300 * time.Millisecond
	if v := os.Getenv("STEP_DELAY_MS"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil {
			delay = time.Duration(ms) * time.Millisecond
		}
	}

	dbPath := "connections.db"
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "connections.db not found; run from backend dir")
		os.Exit(1)
	}

	// Find the WS-active session
	sid, err := findActiveSession()
	if err != nil {
		fmt.Fprintf(os.Stderr, "find session: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("solving session %s\n", sid)

	// Get the puzzle ID for this session
	puzzleID, err := getPuzzleID(sid)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get puzzle id: %v\n", err)
		os.Exit(1)
	}

	// Read categories directly from DB
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open db: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	cats, err := loadCategories(db, puzzleID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load categories: %v\n", err)
		os.Exit(1)
	}

	// Submit each group as a correct guess
	for _, cat := range cats {
		words := make([]string, len(cat.Cards))
		for i, c := range cat.Cards {
			words[i] = c.Content
		}
		fmt.Printf("guessing group %q: %v\n", cat.Title, words)
		if err := submitGuess(sid, words); err != nil {
			fmt.Fprintf(os.Stderr, "guess error: %v\n", err)
			os.Exit(1)
		}
		time.Sleep(delay)
	}

	fmt.Println("done")
}

func findActiveSession() (string, error) {
	resp, err := http.Get(baseURL + "/api/sessions")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var sessions []sessionInfo
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		return "", err
	}
	for _, s := range sessions {
		if s.WSActive {
			return s.ID, nil
		}
	}
	// Fall back to the most recent session if none is WS-active
	if len(sessions) > 0 {
		return sessions[0].ID, nil
	}
	return "", fmt.Errorf("no sessions found")
}

func getPuzzleID(sid string) (int64, error) {
	resp, err := http.Get(baseURL + "/api/session/" + sid)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var s sessionState
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return 0, err
	}
	return s.PuzzleID, nil
}

func loadCategories(db *sql.DB, puzzleID int64) ([]category, error) {
	var dataJSON string
	err := db.QueryRow(`SELECT data FROM puzzles WHERE id=?`, puzzleID).Scan(&dataJSON)
	if err != nil {
		return nil, fmt.Errorf("puzzle %d: %w", puzzleID, err)
	}
	var puzzle struct {
		Categories []category `json:"categories"`
	}
	if err := json.Unmarshal([]byte(dataJSON), &puzzle); err != nil {
		return nil, err
	}
	return puzzle.Categories, nil
}

func submitGuess(sid string, words []string) error {
	body, _ := json.Marshal(map[string]interface{}{"words": words})
	resp, err := http.Post(
		baseURL+"/api/session/"+sid+"/guess",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, b)
	}
	return nil
}
