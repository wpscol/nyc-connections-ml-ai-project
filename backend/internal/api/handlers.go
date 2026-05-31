package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	appdb "connections/internal/db"
	"connections/internal/game"
)

type Server struct {
	db  *sql.DB
	hub *Hub
}

func NewServer(db *sql.DB, hub *Hub) *Server {
	return &Server{db: db, hub: hub}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(corsMiddleware)

	r.Get("/api/puzzle/current", s.currentPuzzle)
	r.Get("/api/sessions", s.listSessions)
	r.Delete("/api/sessions", s.deleteAllSessions)
	r.Post("/api/session", s.createSession)
	r.Get("/api/session/{id}", s.getSession)
	r.Delete("/api/session/{id}", s.deleteSession)
	r.Post("/api/session/{id}/guess", s.submitGuess)
	r.Post("/api/session/{id}/restart", s.restartSession)
	r.Post("/api/session/{id}/next", s.nextSession)
	r.Post("/api/session/{id}/prev", s.prevSession)
	r.Put("/api/config/max_mistakes", s.setMaxMistakes)
	r.Get("/ws", s.hub.ServeWS)

	return r
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// SessionInfo is returned by ListSessions and the /api/sessions endpoint.
type SessionInfo struct {
	ID         string `json:"session_id"`
	Status     string `json:"status"`
	PuzzleDate string `json:"puzzle_date"`
	WSActive   bool   `json:"ws_active"`
}

// ListSessions returns up to 20 recent sessions with ws_active flag.
func (s *Server) ListSessions() ([]SessionInfo, error) {
	rows, err := s.db.Query(`
		SELECT s.id, s.state, COALESCE(p.date, '')
		FROM sessions s
		LEFT JOIN puzzles p ON s.puzzle_id = p.id
		ORDER BY s.created_at DESC LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	active := make(map[string]bool)
	for _, id := range s.hub.ActiveSessions() {
		active[id] = true
	}

	var out []SessionInfo
	for rows.Next() {
		var id, stateJSON, puzzleDate string
		rows.Scan(&id, &stateJSON, &puzzleDate)
		var st game.GameState
		json.Unmarshal([]byte(stateJSON), &st)
		out = append(out, SessionInfo{ID: id, Status: st.Status, PuzzleDate: puzzleDate, WSActive: active[id]})
	}
	if out == nil {
		out = []SessionInfo{}
	}
	return out, nil
}

// SetMaxMistakes persists the new limit and notifies all active browser sessions.
func (s *Server) SetMaxMistakes(count int) error {
	if err := appdb.SetConfig(s.db, "max_mistakes", strconv.Itoa(count)); err != nil {
		return err
	}
	s.hub.BroadcastAll(game.WSEvent{
		Type:    "config_update",
		Payload: map[string]int{"max_mistakes": count},
	})
	return nil
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	out, err := s.ListSessions()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, out)
}

func (s *Server) currentPuzzle(w http.ResponseWriter, r *http.Request) {
	puzzle, err := s.loadCurrentPuzzle()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Return puzzle without answers - only shuffled words
	words := game.AllWords(*puzzle)
	jsonOK(w, map[string]interface{}{
		"id":    puzzle.ID,
		"date":  puzzle.Date,
		"words": words,
	})
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	puzzle, err := s.loadCurrentPuzzle()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	maxMistakes := s.loadMaxMistakes()
	id, state, err := game.CreateSession(s.db, *puzzle, maxMistakes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, stateResponseWithDate(id, state, puzzle.Date))
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	state, err := game.GetSession(s.db, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	date := ""
	if puzzle, perr := s.loadPuzzleByID(state.PuzzleID); perr == nil {
		date = puzzle.Date
	}
	jsonOK(w, stateResponseWithDate(id, state, date))
}

func (s *Server) submitGuess(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		Words []string `json:"words"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Words) != 4 {
		http.Error(w, "need exactly 4 words", http.StatusBadRequest)
		return
	}

	source := r.Header.Get("X-Source")
	if source == "" {
		source = "api"
	}

	result, err := s.processGuess(id, body.Words, source)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.hub.Broadcast(id, game.WSEvent{Type: "guess_result", Payload: result})
	s.broadcastIfComplete(id)

	jsonOK(w, result)
}

// broadcastIfComplete sends a game_complete event with stats when a game ends.
func (s *Server) broadcastIfComplete(id string) {
	state, _ := game.GetSession(s.db, id)
	if state == nil || (state.Status != "won" && state.Status != "lost") {
		return
	}
	s.hub.Broadcast(id, game.WSEvent{
		Type: "game_complete",
		Payload: map[string]interface{}{
			"won":    state.Status == "won",
			"solved": state.Solved,
			"stats":  game.ComputeStats(state),
		},
	})
}

func (s *Server) restartSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	state, err := game.GetSession(s.db, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	puzzle, err := s.loadPuzzleByID(state.PuzzleID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	newState, err := game.ResetSession(s.db, id, *puzzle, s.loadMaxMistakes())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp := stateResponseWithDate(id, newState, puzzle.Date)
	s.hub.Broadcast(id, game.WSEvent{Type: "session_reset", Payload: resp})
	jsonOK(w, resp)
}

func (s *Server) nextSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	state, err := game.GetSession(s.db, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	nextID := s.nextPuzzleID(state.PuzzleID)
	puzzle, err := s.loadPuzzleByID(nextID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	newState, err := game.ResetSession(s.db, id, *puzzle, s.loadMaxMistakes())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Advance the global pointer so brand-new sessions follow along
	appdb.SetConfig(s.db, "current_puzzle_id", strconv.FormatInt(nextID, 10))

	resp := stateResponseWithDate(id, newState, puzzle.Date)
	s.hub.Broadcast(id, game.WSEvent{Type: "session_reset", Payload: resp})
	jsonOK(w, resp)
}

func (s *Server) prevSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	state, err := game.GetSession(s.db, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	prevID := s.prevPuzzleID(state.PuzzleID)
	puzzle, err := s.loadPuzzleByID(prevID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	newState, err := game.ResetSession(s.db, id, *puzzle, s.loadMaxMistakes())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	appdb.SetConfig(s.db, "current_puzzle_id", strconv.FormatInt(prevID, 10))

	resp := stateResponseWithDate(id, newState, puzzle.Date)
	s.hub.Broadcast(id, game.WSEvent{Type: "session_reset", Payload: resp})
	jsonOK(w, resp)
}

func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, err := s.db.Exec(`DELETE FROM sessions WHERE id=?`, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteAllSessions(w http.ResponseWriter, r *http.Request) {
	if _, err := s.db.Exec(`DELETE FROM sessions`); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setMaxMistakes(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Count < 1 || body.Count > 10 {
		http.Error(w, "count must be 1-10", http.StatusBadRequest)
		return
	}
	if err := s.SetMaxMistakes(body.Count); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]int{"max_mistakes": body.Count})
}

// processGuess is shared between REST handler and MCP tool.
func (s *Server) processGuess(sessionID string, words []string, source string) (*game.GuessResult, error) {
	state, err := game.GetSession(s.db, sessionID)
	if err != nil {
		return nil, err
	}
	if state.Status != "playing" {
		return nil, fmt.Errorf("game is %s", state.Status)
	}

	puzzle, err := s.loadPuzzleByID(state.PuzzleID)
	if err != nil {
		return nil, err
	}

	solved := game.SolvedDifficulties(state.Solved)
	correct, cat, oneAway := game.ValidateGuess(words, *puzzle, solved)

	result := &game.GuessResult{
		Correct:      correct,
		OneAway:      oneAway,
		MistakesLeft: state.MistakesLeft,
		Status:       state.Status,
		Guessed:      words,
		Source:       source,
	}
	difficulty := -1
	if correct {
		sg := &game.SolvedGroup{
			Title:      cat.Title,
			Difficulty: cat.Difficulty,
		}
		for _, c := range cat.Cards {
			sg.Words = append(sg.Words, c.Content)
		}
		result.Category = sg
		difficulty = cat.Difficulty
	}

	game.ApplyGuess(state, result, cat)
	result.MistakesLeft = state.MistakesLeft
	result.Status = state.Status

	// Record the attempt for the live log
	state.Guesses = append(state.Guesses, game.GuessAttempt{
		Words:      words,
		Correct:    correct,
		OneAway:    oneAway,
		Difficulty: difficulty,
		Source:     source,
		At:         time.Now().UnixMilli(),
	})

	if err := game.SaveSession(s.db, sessionID, state); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Server) loadCurrentPuzzle() (*game.Puzzle, error) {
	idStr, err := appdb.GetConfig(s.db, "current_puzzle_id")
	if err != nil || idStr == "" {
		return nil, fmt.Errorf("no current puzzle configured")
	}
	id, _ := strconv.ParseInt(idStr, 10, 64)
	return s.loadPuzzleByID(id)
}

func (s *Server) loadPuzzleByID(id int64) (*game.Puzzle, error) {
	var dataJSON string
	err := s.db.QueryRow(`SELECT data FROM puzzles WHERE id=?`, id).Scan(&dataJSON)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("puzzle %d not found", id)
	}
	if err != nil {
		return nil, err
	}
	var raw struct {
		ID         int64            `json:"id"`
		Date       string           `json:"date"`
		Categories []game.Category  `json:"categories"`
	}
	if err := json.Unmarshal([]byte(dataJSON), &raw); err != nil {
		return nil, err
	}
	return &game.Puzzle{ID: raw.ID, Date: raw.Date, Categories: raw.Categories}, nil
}

func (s *Server) loadMaxMistakes() int {
	v, _ := appdb.GetConfig(s.db, "max_mistakes")
	if v == "" {
		return 4
	}
	n, _ := strconv.Atoi(v)
	if n < 1 {
		return 4
	}
	return n
}

// CreateSession creates a new session for the current puzzle and returns its ID and state.
// Exported for the MCP server.
func (s *Server) CreateSession() (string, *game.GameState, string, error) {
	puzzle, err := s.loadCurrentPuzzle()
	if err != nil {
		return "", nil, "", err
	}
	id, state, err := game.CreateSession(s.db, *puzzle, s.loadMaxMistakes())
	if err != nil {
		return "", nil, "", err
	}
	return id, state, puzzle.Date, nil
}

// ProcessGuess is exported for use by MCP server.
func (s *Server) ProcessGuess(sessionID string, words []string, source string) (*game.GuessResult, error) {
	return s.processGuess(sessionID, words, source)
}

// GetSession is exported for use by MCP server.
func (s *Server) GetSession(sessionID string) (*game.GameState, error) {
	return game.GetSession(s.db, sessionID)
}

// BroadcastIfComplete is exported for use by MCP server.
func (s *Server) BroadcastIfComplete(sessionID string) {
	s.broadcastIfComplete(sessionID)
}

// RestartSession resets the session to its current puzzle and broadcasts.
// Exported for the MCP server.
func (s *Server) RestartSession(sessionID string) (*game.GameState, error) {
	state, err := game.GetSession(s.db, sessionID)
	if err != nil {
		return nil, err
	}
	puzzle, err := s.loadPuzzleByID(state.PuzzleID)
	if err != nil {
		return nil, err
	}
	newState, err := game.ResetSession(s.db, sessionID, *puzzle, s.loadMaxMistakes())
	if err != nil {
		return nil, err
	}
	s.hub.Broadcast(sessionID, game.WSEvent{
		Type:    "session_reset",
		Payload: stateResponseWithDate(sessionID, newState, puzzle.Date),
	})
	return newState, nil
}

// NextSession advances the session to the next puzzle and broadcasts.
// Exported for the MCP server.
func (s *Server) NextSession(sessionID string) (*game.GameState, error) {
	state, err := game.GetSession(s.db, sessionID)
	if err != nil {
		return nil, err
	}
	nextID := s.nextPuzzleID(state.PuzzleID)
	puzzle, err := s.loadPuzzleByID(nextID)
	if err != nil {
		return nil, err
	}
	newState, err := game.ResetSession(s.db, sessionID, *puzzle, s.loadMaxMistakes())
	if err != nil {
		return nil, err
	}
	appdb.SetConfig(s.db, "current_puzzle_id", strconv.FormatInt(nextID, 10))
	s.hub.Broadcast(sessionID, game.WSEvent{
		Type:    "session_reset",
		Payload: stateResponseWithDate(sessionID, newState, puzzle.Date),
	})
	return newState, nil
}

// PrevSession rewinds the session to the previous puzzle and broadcasts.
// Exported for the MCP server.
func (s *Server) PrevSession(sessionID string) (*game.GameState, error) {
	state, err := game.GetSession(s.db, sessionID)
	if err != nil {
		return nil, err
	}
	prevID := s.prevPuzzleID(state.PuzzleID)
	puzzle, err := s.loadPuzzleByID(prevID)
	if err != nil {
		return nil, err
	}
	newState, err := game.ResetSession(s.db, sessionID, *puzzle, s.loadMaxMistakes())
	if err != nil {
		return nil, err
	}
	appdb.SetConfig(s.db, "current_puzzle_id", strconv.FormatInt(prevID, 10))
	s.hub.Broadcast(sessionID, game.WSEvent{
		Type:    "session_reset",
		Payload: stateResponseWithDate(sessionID, newState, puzzle.Date),
	})
	return newState, nil
}

// nextPuzzleID returns the next puzzle id after current, wrapping to the first.
func (s *Server) nextPuzzleID(current int64) int64 {
	var nextID int64
	err := s.db.QueryRow(`SELECT id FROM puzzles WHERE id > ? ORDER BY id ASC LIMIT 1`, current).Scan(&nextID)
	if err == sql.ErrNoRows || nextID == 0 {
		s.db.QueryRow(`SELECT id FROM puzzles ORDER BY id ASC LIMIT 1`).Scan(&nextID)
	}
	return nextID
}

// prevPuzzleID returns the previous puzzle id before current, wrapping to the last.
func (s *Server) prevPuzzleID(current int64) int64 {
	var prevID int64
	err := s.db.QueryRow(`SELECT id FROM puzzles WHERE id < ? ORDER BY id DESC LIMIT 1`, current).Scan(&prevID)
	if err == sql.ErrNoRows || prevID == 0 {
		s.db.QueryRow(`SELECT id FROM puzzles ORDER BY id DESC LIMIT 1`).Scan(&prevID)
	}
	return prevID
}

func (s *Server) LoadCurrentPuzzle() (*game.Puzzle, error) {
	return s.loadCurrentPuzzle()
}

func (s *Server) LoadMaxMistakes() int {
	return s.loadMaxMistakes()
}

func stateResponse(id string, state *game.GameState) map[string]interface{} {
	return stateResponseWithDate(id, state, "")
}

func stateResponseWithDate(id string, state *game.GameState, date string) map[string]interface{} {
	resp := map[string]interface{}{
		"session_id":    id,
		"puzzle_id":     state.PuzzleID,
		"remaining":     state.RemainingWords,
		"solved":        state.Solved,
		"mistakes_left": state.MistakesLeft,
		"max_mistakes":  state.MaxMistakes,
		"status":        state.Status,
		"guesses":       state.Guesses,
		"stats":         game.ComputeStats(state),
	}
	if date != "" {
		resp["date"] = date
	}
	return resp
}

func jsonOK(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
