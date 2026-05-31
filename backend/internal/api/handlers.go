package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

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
	r.Post("/api/session", s.createSession)
	r.Get("/api/session/{id}", s.getSession)
	r.Post("/api/session/{id}/guess", s.submitGuess)
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

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT id, state FROM sessions ORDER BY created_at DESC LIMIT 20`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// Build a set of session IDs that currently have an open browser WS connection
	active := make(map[string]bool)
	for _, id := range s.hub.ActiveSessions() {
		active[id] = true
	}

	type info struct {
		ID       string `json:"session_id"`
		Status   string `json:"status"`
		WSActive bool   `json:"ws_active"`
	}
	var out []info
	for rows.Next() {
		var id, stateJSON string
		rows.Scan(&id, &stateJSON)
		var st game.GameState
		json.Unmarshal([]byte(stateJSON), &st)
		out = append(out, info{ID: id, Status: st.Status, WSActive: active[id]})
	}
	if out == nil {
		out = []info{}
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
	jsonOK(w, map[string]interface{}{
		"session_id":    id,
		"puzzle_id":     state.PuzzleID,
		"date":          puzzle.Date,
		"remaining":     state.RemainingWords,
		"solved":        state.Solved,
		"mistakes_left": state.MistakesLeft,
		"max_mistakes":  state.MaxMistakes,
		"status":        state.Status,
	})
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	state, err := game.GetSession(s.db, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	jsonOK(w, stateResponse(id, state))
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

	result, err := s.processGuess(id, body.Words)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.hub.Broadcast(id, game.WSEvent{Type: "guess_result", Payload: result})

	state, _ := game.GetSession(s.db, id)
	if state != nil && (state.Status == "won" || state.Status == "lost") {
		s.hub.Broadcast(id, game.WSEvent{
			Type: "game_complete",
			Payload: map[string]interface{}{
				"won":    state.Status == "won",
				"solved": state.Solved,
			},
		})
		if state.Status == "won" {
			s.advancePuzzle()
		}
	}

	jsonOK(w, result)
}

func (s *Server) setMaxMistakes(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Count < 1 || body.Count > 10 {
		http.Error(w, "count must be 1-10", http.StatusBadRequest)
		return
	}
	if err := appdb.SetConfig(s.db, "max_mistakes", strconv.Itoa(body.Count)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]int{"max_mistakes": body.Count})
}

// processGuess is shared between REST handler and MCP tool.
func (s *Server) processGuess(sessionID string, words []string) (*game.GuessResult, error) {
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
	}
	if correct {
		sg := &game.SolvedGroup{
			Title:      cat.Title,
			Difficulty: cat.Difficulty,
		}
		for _, c := range cat.Cards {
			sg.Words = append(sg.Words, c.Content)
		}
		result.Category = sg
	}

	game.ApplyGuess(state, result, cat)
	result.MistakesLeft = state.MistakesLeft
	result.Status = state.Status

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

func (s *Server) advancePuzzle() {
	idStr, _ := appdb.GetConfig(s.db, "current_puzzle_id")
	id, _ := strconv.ParseInt(idStr, 10, 64)
	var nextID int64
	s.db.QueryRow(`SELECT id FROM puzzles WHERE id > ? ORDER BY id ASC LIMIT 1`, id).Scan(&nextID)
	if nextID > 0 {
		appdb.SetConfig(s.db, "current_puzzle_id", strconv.FormatInt(nextID, 10))
	}
}

// ProcessGuess is exported for use by MCP server.
func (s *Server) ProcessGuess(sessionID string, words []string) (*game.GuessResult, error) {
	return s.processGuess(sessionID, words)
}

// GetSession is exported for use by MCP server.
func (s *Server) GetSession(sessionID string) (*game.GameState, error) {
	return game.GetSession(s.db, sessionID)
}

func (s *Server) LoadCurrentPuzzle() (*game.Puzzle, error) {
	return s.loadCurrentPuzzle()
}

func (s *Server) LoadMaxMistakes() int {
	return s.loadMaxMistakes()
}

func (s *Server) AdvancePuzzle() {
	s.advancePuzzle()
}

func stateResponse(id string, state *game.GameState) map[string]interface{} {
	return map[string]interface{}{
		"session_id":    id,
		"puzzle_id":     state.PuzzleID,
		"remaining":     state.RemainingWords,
		"solved":        state.Solved,
		"mistakes_left": state.MistakesLeft,
		"max_mistakes":  state.MaxMistakes,
		"status":        state.Status,
	}
}

func jsonOK(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
