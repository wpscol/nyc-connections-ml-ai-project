package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"connections/internal/api"
	"connections/internal/embeddings"
	"connections/internal/game"
)

// MemNote is a single agent memory entry.
type MemNote struct {
	Key       string `json:"key"`
	Content   string `json:"content"`
	UpdatedAt int64  `json:"updated_at"` // unix ms
}

// MemoryStore is a simple in-process key→note store shared across tool calls.
// It is exported so the REST layer can serve its contents.
type MemoryStore struct {
	mu    sync.Mutex
	notes map[string]MemNote
}

func newMemoryStore() *MemoryStore {
	return &MemoryStore{notes: make(map[string]MemNote)}
}

func (m *MemoryStore) write(key, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notes[key] = MemNote{Key: key, Content: content, UpdatedAt: time.Now().UnixMilli()}
}

func (m *MemoryStore) list() []MemNote {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MemNote, 0, len(m.notes))
	for _, n := range m.notes {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out
}

func (m *MemoryStore) delete(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.notes[key]
	if ok {
		delete(m.notes, key)
	}
	return ok
}

func (m *MemoryStore) clear() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := len(m.notes)
	m.notes = make(map[string]MemNote)
	return n
}

// Page returns a paginated slice of notes (newest first) and the total count.
// page is 1-based; perPage must be ≥ 1.
func (m *MemoryStore) Page(page, perPage int) ([]MemNote, int) {
	all := m.list()
	total := len(all)
	if perPage < 1 {
		perPage = 10
	}
	start := (page - 1) * perPage
	if start >= total {
		return []MemNote{}, total
	}
	end := start + perPage
	if end > total {
		end = total
	}
	return all[start:end], total
}

type gameAPI interface {
	CreateSession() (id string, state *game.GameState, date string, err error)
	ProcessGuess(sessionID string, words []string, source string) (*game.GuessResult, error)
	GetSession(sessionID string) (*game.GameState, error)
	GetGuesses(sessionID string) ([]game.GuessAttempt, error)
	LoadCurrentPuzzle() (*game.Puzzle, error)
	LoadMaxMistakes() int
	ListSessions() ([]api.SessionInfo, error)
	BroadcastIfComplete(sessionID string)
	RestartSession(sessionID string) (*game.GameState, error)
	NextSession(sessionID string) (*game.GameState, error)
	PrevSession(sessionID string) (*game.GameState, error)
}

const serverInstructions = `# NYT Connections — AI Solver Guide

## What is this game?
You are playing NYT Connections: a word-grouping puzzle. A 4×4 board of 16 words hides exactly 4 secret groups of 4 words each. Every word belongs to exactly one group. Your goal is to identify all 4 groups before running out of mistakes.

## Difficulty levels (colour-coded)
Each group has a difficulty that tells you how tricky the connection is:
- 0 = Yellow  — straightforward, literal category (e.g. "Types of dog breed")
- 1 = Green   — slightly less obvious
- 2 = Blue    — requires lateral thinking
- 3 = Purple  — most devious; often a wordplay, double-meaning, or "___ + word" pattern

Always try to solve easier groups first. A correct Purple guess early is risky; a wrong guess wastes a precious mistake.

## Winning and losing
- You start with max_mistakes chances (default 4).
- Each wrong guess costs exactly 1 mistake.
- Solve all 4 groups → status becomes "won".
- Run out of mistakes before solving all groups → status becomes "lost".

## Key response fields
- status: "playing" | "won" | "lost"
- mistakes_left: how many wrong guesses remain
- one_away: true means exactly 3 of your 4 words were in the right group — swap one word and retry
- correct: true means the group was found; the category title and its 4 words are revealed
- remaining: words still on the board (use this to choose your next guess)
- solved: groups already found (their words are gone from the board)
- tried_combinations: EVERY guess made so far this game — result is "correct", "one_away", or "wrong"

## Tried combinations — MANDATORY pre-guess check
get_state returns a tried_combinations list. BEFORE every submit_guess call you MUST:
1. Read tried_combinations in full.
2. Confirm your intended 4 words do NOT match any entry in that list (order does not matter — same set = duplicate).
3. For one_away entries: identify which 3 words are likely correct (keep them), then try a different 4th word.
4. For wrong entries: treat ALL 4 words as suspects — they likely span 2 groups; do not reuse that set.
Submitting a duplicate combination is a wasted mistake. Never do it.

## Solving strategy
1. Call list_sessions — join an existing session (ws_active=true preferred). You cannot create sessions.
2. Call get_state — read remaining words AND tried_combinations carefully.
3. Check tried_combinations: rule out any set you already know is wrong or one_away.
4. Look for the most obvious group first (start with Yellow-level thinking):
   - Literal sets: "___ fish", "things in a kitchen", "shades of blue"
   - Shared prefix/suffix: all can follow "OVER___" or precede "___HOUSE"
   - Famous groups: "Beatles members", "US presidents"
5. Pick the group you are MOST confident about — verify it is not in tried_combinations — then call submit_guess.
6. Read the result:
   - correct=true → call get_state again and repeat from step 2.
   - one_away=true → swap exactly one word (not all four) and retry.
   - correct=false, one_away=false → reconsider entirely; those 4 words likely span 2+ groups.
7. If only 2 groups remain, the last one is forced — no need to guess it separately.

## After a game ends
- WON → call next_game to advance to the next puzzle.
- LOST → call restart_game to replay the same puzzle. Keep retrying until you win. Never skip a puzzle you lost.

## Red-herring warning
The puzzle deliberately puts misleading words on the board. A word that "obviously" fits one group may actually belong to another. When you are unsure between two groups, solve the one you are 100% sure of first to shrink the board.

## Semantic embedding support (suggest_groups)
When unsure about a grouping, call suggest_groups with the remaining words and the number of unsolved groups.
It embeds every word using a local language model and clusters them by semantic similarity.

Key output fields per group:
- avg_similarity: mean pairwise cosine similarity (0–1). >0.80 = tight cluster; 0.60–0.80 = moderate; <0.60 = loose.
- min_similarity: the weakest pair in the group — the word closest to that minimum is the most likely misfit to swap.
- confidence: composite score (0.65×avg + 0.35×min) — higher is more trustworthy.
- overall_quality: silhouette coefficient (-1 to 1). >0.5 = clean separation; 0.2–0.5 = OK; <0.2 = ambiguous.

Important limitations:
- Embeddings capture SEMANTIC meaning (usage context), not puzzle wordplay, puns, or "___ + word" fill-in patterns.
- A Purple-difficulty group may look semantically scattered but still be the correct answer.
- Use suggest_groups as a SECOND OPINION, not as truth. Cross-reference with tried_combinations before acting.
- If suggest_groups and your own reasoning agree on a group → high confidence to submit.
- If they disagree → solve a different group you are certain about first to shrink the board.

## Tool workflow
list_sessions → get_state → (check tried_combinations) → (reason, optionally suggest_groups) → submit_guess → (repeat) → won: next_game | lost: restart_game`

// refuseIfNotWon gates puzzle-navigation tools (next_game / prev_game): the model
// may move to another puzzle ONLY after winning the current one. Returns ("", true)
// when the call may proceed, or (errorMessage, false) when it must be refused.
func refuseIfNotWon(srv gameAPI, sessionID, tool string) (string, bool) {
	state, err := srv.GetSession(sessionID)
	if err != nil {
		return fmt.Sprintf("session not found: %s", err), false
	}
	if state.Status == "won" {
		return "", true
	}
	if state.Status == "lost" {
		return fmt.Sprintf("%s refused: this puzzle is LOST, not won — call restart_game and keep retrying until you win. You may never skip a puzzle you have not won.", tool), false
	}
	return fmt.Sprintf("%s refused: this puzzle is still in progress (status=%q), not won — keep guessing until you win. You may never skip a puzzle you have not won.", tool, state.Status), false
}

// stateTip builds the get_state next-step hint for a non-won game (a won game
// short-circuits in the handler with its own minimal message). A lost game
// points at restart_game; a live game summarises progress.
func stateTip(state *game.GameState, triedCount int) string {
	switch state.Status {
	case "won":
		return "This puzzle is already WON — do not analyse or guess. Call next_game to advance to the next puzzle."
	case "lost":
		return "Game over. Call restart_game — never skip a puzzle you lost. tried_combinations carries over so you can see what already worked."
	default:
		remaining := len(state.RemainingWords)
		return fmt.Sprintf(
			"%d words remain in %d unsolved group(s). %d mistake(s) left. "+
				"%d attempt(s) total (including previous restarts) — NEVER resubmit any set already in tried_combinations.",
			remaining, remaining/4, state.MistakesLeft, triedCount,
		)
	}
}

// allGuesses merges prior_guesses + guesses so the model sees the full history
// including across restarts of the same puzzle.
func allGuesses(state *game.GameState) []game.GuessAttempt {
	out := make([]game.GuessAttempt, 0, len(state.PriorGuesses)+len(state.Guesses))
	out = append(out, state.PriorGuesses...)
	out = append(out, state.Guesses...)
	return out
}

// formatTriedCombinations converts the raw GuessAttempt slice into a compact
// list suitable for the MCP response. Each entry carries the words, a plain
// "result" string, and (for correct guesses) the difficulty level.
func formatTriedCombinations(guesses []game.GuessAttempt) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(guesses))
	for _, g := range guesses {
		entry := map[string]interface{}{
			"words": g.Words,
		}
		switch {
		case g.Correct:
			entry["result"] = "correct"
			entry["difficulty"] = g.Difficulty
		case g.OneAway:
			entry["result"] = "one_away"
		default:
			entry["result"] = "wrong"
		}
		out = append(out, entry)
	}
	return out
}

// Build creates the SSE HTTP handler for the MCP server and returns it together
// with the MemoryStore so callers can expose its contents over REST.
// embedClient may be nil — the suggest_groups tool will report unavailable in that case.
func Build(srv gameAPI, hub *api.Hub, baseURL string, embedClient *embeddings.Client) (http.Handler, *MemoryStore) {
	mem := newMemoryStore()

	s := server.NewMCPServer(
		"connections-game",
		"1.0.0",
		server.WithToolCapabilities(false),
		server.WithInstructions(serverInstructions),
	)

	// ── list_sessions ─────────────────────────────────────────────────────────
	s.AddTool(mcpgo.NewTool("list_sessions",
		mcpgo.WithDescription(
			"List all existing game sessions. ALWAYS call this first — you can only join existing sessions, not create new ones. "+
				"Prefer ws_active=true sessions (human browser is watching). "+
				"If no sessions exist, ask the human to open the browser first and try again — do not attempt to create a session. "+
				"status: 'playing' = active, 'won' = solved (next_game allowed), 'lost' = failed (restart_game, never next_game — skipping an unwon puzzle is refused by the server).",
		),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		sessions, err := srv.ListSessions()
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		tip := "Join a session with ws_active=true. status='playing' → keep solving; status='won' → next_game; status='lost' → restart_game (you may NOT next_game an unwon puzzle)."
		if len(sessions) == 0 {
			tip = "No sessions found. Ask the human to open the game in their browser, then call list_sessions again."
		}
		out := map[string]interface{}{
			"sessions": sessions,
			"tip":      tip,
		}
		data, _ := json.Marshal(out)
		return mcpgo.NewToolResultText(string(data)), nil
	})

	// ── get_state ─────────────────────────────────────────────────────────────
	s.AddTool(mcpgo.NewTool("get_state",
		mcpgo.WithDescription(
			"Get the full current state of a game session: remaining words, solved groups, "+
				"mistakes left, game status, and the complete tried_combinations history. "+
				"Call this at the start of every turn and after every guess. "+
				"BEFORE calling submit_guess you MUST read tried_combinations and confirm your "+
				"intended 4 words do not duplicate any previous attempt (order-independent). "+
				"tried_combinations entries: result='correct' (group solved), 'one_away' (swap one word), "+
				"'wrong' (entirely wrong set — do not reuse any of those 4 words together again).",
		),
		mcpgo.WithString("session_id", mcpgo.Required(), mcpgo.Description("Session ID returned by new_game or list_sessions")),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()
		sessionID, _ := args["session_id"].(string)
		if sessionID == "" {
			return mcpgo.NewToolResultError("session_id required"), nil
		}
		state, err := srv.GetSession(sessionID)
		if err != nil {
			return mcpgo.NewToolResultError(fmt.Sprintf("session not found: %s", err)), nil
		}
		hub.Broadcast(sessionID, game.WSEvent{Type: "state_sync", Payload: map[string]string{"session_id": sessionID}})

		// A won puzzle is finished — just say so. Don't dump the board, solved
		// groups, or tried_combinations: there is nothing left to analyse, the
		// only valid move is next_game. This stays true until next_game advances.
		if state.Status == "won" {
			out := map[string]interface{}{
				"session_id": sessionID,
				"status":     "won",
				"game_over":  true,
				"tip":        "This puzzle is already WON — do not analyse or guess. Call next_game to advance to the next puzzle.",
			}
			data, _ := json.Marshal(out)
			return mcpgo.NewToolResultText(string(data)), nil
		}

		tried := formatTriedCombinations(allGuesses(state))

		out := map[string]interface{}{
			"session_id":         sessionID,
			"remaining":          state.RemainingWords,
			"solved":             state.Solved,
			"mistakes_left":      state.MistakesLeft,
			"max_mistakes":       state.MaxMistakes,
			"status":             state.Status,
			"tried_combinations": tried,
			"tip":                stateTip(state, len(tried)),
		}
		data, _ := json.Marshal(out)
		return mcpgo.NewToolResultText(string(data)), nil
	})

	// ── get_board ─────────────────────────────────────────────────────────────
	s.AddTool(mcpgo.NewTool("get_board",
		mcpgo.WithDescription(
			"Lightweight board view: remaining words, solved groups, and tried_combinations history. "+
				"Use get_state when you also need mistakes_left and status. "+
				"Always check tried_combinations before calling submit_guess — never duplicate a prior attempt.",
		),
		mcpgo.WithString("session_id", mcpgo.Required(), mcpgo.Description("Session ID")),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()
		sessionID, _ := args["session_id"].(string)
		if sessionID == "" {
			return mcpgo.NewToolResultError("session_id required"), nil
		}
		state, err := srv.GetSession(sessionID)
		if err != nil {
			return mcpgo.NewToolResultError(fmt.Sprintf("session not found: %s", err)), nil
		}
		tried := formatTriedCombinations(allGuesses(state))
		out := map[string]interface{}{
			"remaining":          state.RemainingWords,
			"solved":             state.Solved,
			"tried_combinations": tried,
			"tip":                "tried_combinations includes ALL attempts across restarts. Never resubmit any set in this list. correct=true entries show which words are confirmed grouped — use them to anchor your next guess.",
		}
		data, _ := json.Marshal(out)
		return mcpgo.NewToolResultText(string(data)), nil
	})

	// ── submit_guess ──────────────────────────────────────────────────────────
	s.AddTool(mcpgo.NewTool("submit_guess",
		mcpgo.WithDescription(
			"Submit exactly 4 words as a single group guess. "+
				"PRE-FLIGHT CHECK (mandatory): read tried_combinations from get_state and verify your 4 words do NOT "+
				"match any entry — comparison is ORDER-INDEPENDENT: {A,B,C,D} == {D,B,A,C} == any permutation. "+
				"If you are even slightly unsure whether you already tried a combination, call get_state first — never guess from memory. "+
				"Submitting a duplicate wastes a mistake. "+
				"Result interpretation: "+
				"correct=true → group solved, category revealed, words removed from board; "+
				"one_away=true → 3 of your 4 words were right, swap exactly one word and retry; "+
				"correct=false, one_away=false → entirely wrong set, those 4 words span 2+ groups. "+
				"RULE: submit ONE group per call. Call get_state after every guess before the next attempt.",
		),
		mcpgo.WithString("session_id", mcpgo.Required(), mcpgo.Description("Session ID")),
		mcpgo.WithString("words", mcpgo.Required(), mcpgo.Description(
			"Exactly 4 comma-separated words from the remaining board, "+
				"e.g. PUMP,BOOT,MILE,SNEAKER — use the exact spelling shown in get_state",
		)),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()
		sessionID, _ := args["session_id"].(string)
		wordsStr, _ := args["words"].(string)
		if sessionID == "" || wordsStr == "" {
			return mcpgo.NewToolResultError("session_id and words required"), nil
		}
		parts := strings.Split(wordsStr, ",")
		if len(parts) != 4 {
			return mcpgo.NewToolResultError("exactly 4 comma-separated words required"), nil
		}
		words := make([]string, 4)
		for i, p := range parts {
			words[i] = strings.TrimSpace(strings.ToUpper(p))
		}

		result, err := srv.ProcessGuess(sessionID, words, "mcp")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}

		hub.Broadcast(sessionID, game.WSEvent{Type: "guess_result", Payload: result})
		srv.BroadcastIfComplete(sessionID)

		// Build a human-readable next-step hint based on the result
		var nextStep string
		switch {
		case result.Status == "won":
			nextStep = "Puzzle solved! Call next_game to advance to the next puzzle."
		case result.Status == "lost":
			nextStep = "No mistakes remaining — call restart_game to retry this same puzzle. Never skip a puzzle you lost."
		case result.Correct:
			nextStep = fmt.Sprintf("Group %q solved! Call get_state to see the updated board and pick your next group.", result.Category.Title)
		case result.OneAway:
			nextStep = fmt.Sprintf(
				"One away! 3 of [%s] belong together but one word is wrong. Swap one word and try again.",
				strings.Join(words, ", "),
			)
		default:
			nextStep = fmt.Sprintf(
				"Wrong — those 4 words likely span 2 or more groups. %d mistake(s) left. Call get_state and reconsider.",
				result.MistakesLeft,
			)
		}

		out := map[string]interface{}{
			"correct":       result.Correct,
			"one_away":      result.OneAway,
			"category":      result.Category,
			"mistakes_left": result.MistakesLeft,
			"status":        result.Status,
			"next_step":     nextStep,
		}
		data, _ := json.Marshal(out)
		return mcpgo.NewToolResultText(string(data)), nil
	})

	// ── restart_game ──────────────────────────────────────────────────────────
	s.AddTool(mcpgo.NewTool("restart_game",
		mcpgo.WithDescription(
			"Reset the session to replay the same puzzle from scratch. "+
				"REQUIRED after every loss — never skip a puzzle you failed. Keep restarting until you win it. "+
				"Clears solved groups, restores all 16 words, resets mistakes to the configured maximum. "+
				"The browser UI updates live.",
		),
		mcpgo.WithString("session_id", mcpgo.Required(), mcpgo.Description("Session ID to restart")),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()
		sessionID, _ := args["session_id"].(string)
		if sessionID == "" {
			return mcpgo.NewToolResultError("session_id required"), nil
		}
		state, err := srv.RestartSession(sessionID)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		out := map[string]interface{}{
			"status":       state.Status,
			"remaining":    state.RemainingWords,
			"max_mistakes": state.MaxMistakes,
			"note":         "Puzzle reset. FIRST call get_state — your full tried_combinations history from before the restart is preserved. correct=true entries show confirmed groups; use them to guide your next attempt.",
		}
		data, _ := json.Marshal(out)
		return mcpgo.NewToolResultText(string(data)), nil
	})

	// ── next_game ─────────────────────────────────────────────────────────────
	s.AddTool(mcpgo.NewTool("next_game",
		mcpgo.WithDescription(
			"Advance the session to the next puzzle in the archive. "+
				"ONLY allowed after WINNING the current puzzle (status='won'); the server REFUSES this call otherwise. "+
				"You may never skip a puzzle you have not won — after a loss, call restart_game and keep retrying until you win. "+
				"Updates the global current-puzzle pointer. The browser UI updates live.",
		),
		mcpgo.WithString("session_id", mcpgo.Required(), mcpgo.Description("Session ID to advance")),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()
		sessionID, _ := args["session_id"].(string)
		if sessionID == "" {
			return mcpgo.NewToolResultError("session_id required"), nil
		}
		if msg, ok := refuseIfNotWon(srv, sessionID, "next_game"); !ok {
			return mcpgo.NewToolResultError(msg), nil
		}
		state, err := srv.NextSession(sessionID)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		out := map[string]interface{}{
			"status":    state.Status,
			"puzzle_id": state.PuzzleID,
			"remaining": state.RemainingWords,
			"note":      "Moved to next puzzle. Call get_state to see the new board.",
		}
		data, _ := json.Marshal(out)
		return mcpgo.NewToolResultText(string(data)), nil
	})

	// ── prev_game ─────────────────────────────────────────────────────────────
	s.AddTool(mcpgo.NewTool("prev_game",
		mcpgo.WithDescription(
			"Go back to the previous puzzle in the archive. "+
				"ONLY allowed after WINNING the current puzzle (status='won'); the server REFUSES this call otherwise — "+
				"you may never abandon an unsolved puzzle. "+
				"Updates the global current-puzzle pointer. The browser UI updates live.",
		),
		mcpgo.WithString("session_id", mcpgo.Required(), mcpgo.Description("Session ID to rewind")),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()
		sessionID, _ := args["session_id"].(string)
		if sessionID == "" {
			return mcpgo.NewToolResultError("session_id required"), nil
		}
		if msg, ok := refuseIfNotWon(srv, sessionID, "prev_game"); !ok {
			return mcpgo.NewToolResultError(msg), nil
		}
		state, err := srv.PrevSession(sessionID)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		out := map[string]interface{}{
			"status":    state.Status,
			"puzzle_id": state.PuzzleID,
			"remaining": state.RemainingWords,
			"note":      "Moved to previous puzzle. Call get_state to see the board.",
		}
		data, _ := json.Marshal(out)
		return mcpgo.NewToolResultText(string(data)), nil
	})

	// ── suggest_groups ────────────────────────────────────────────────────────
	s.AddTool(mcpgo.NewTool("suggest_groups",
		mcpgo.WithDescription(
			"Cluster words by semantic similarity using local embeddings to suggest likely groupings. "+
				"Pass the REMAINING words from get_state and the number of unsolved groups. "+
				"The tool embeds each word with a language model, runs K-means++ clustering, "+
				"and returns groups ranked by confidence with the following statistics per group: "+
				"avg_similarity (mean pairwise cosine similarity — higher = tighter semantic cluster), "+
				"min_similarity (weakest pair — the word near this value is most likely the misfit to swap), "+
				"confidence (composite score 0–1). "+
				"overall_quality is the silhouette score: >0.5 clean separation, 0.2–0.5 moderate, <0.2 ambiguous. "+
				"LIMITATION: embeddings capture word meaning, not puzzle wordplay or fill-in-the-blank patterns. "+
				"Purple groups may score low despite being correct. "+
				"Use this as a second opinion: if it agrees with your reasoning, submit confidently; "+
				"if it disagrees, solve a group you are certain about first to shrink the board.",
		),
		mcpgo.WithString("words", mcpgo.Required(), mcpgo.Description(
			"Comma-separated words to analyse — use the remaining words from get_state, "+
				"e.g. PUMP,BOOT,MILE,SNEAKER,BANK,RIVER,SHORE,DELTA,ACE,KING,QUEEN,JACK,SPADE,CLUB,HEART,DIAMOND",
		)),
		mcpgo.WithNumber("group_count", mcpgo.Description(
			"Number of groups to find (default 4). Set to the number of unsolved groups remaining.",
		)),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		if embedClient == nil {
			return mcpgo.NewToolResultError(
				"Embedding service not configured. Set EMBED_URL (e.g. http://localhost:1234) and restart the server.",
			), nil
		}

		args := req.GetArguments()
		wordsStr, _ := args["words"].(string)
		if wordsStr == "" {
			return mcpgo.NewToolResultError("words required"), nil
		}

		rawParts := strings.Split(wordsStr, ",")
		words := make([]string, 0, len(rawParts))
		for _, p := range rawParts {
			if w := strings.TrimSpace(strings.ToUpper(p)); w != "" {
				words = append(words, w)
			}
		}
		if len(words) < 2 {
			return mcpgo.NewToolResultError("need at least 2 words to cluster"), nil
		}

		k := 4
		if v, ok := args["group_count"].(float64); ok && v >= 1 {
			k = int(v)
		}
		if k > len(words) {
			k = len(words)
		}

		vecs, err := embedClient.Embed(ctx, words)
		if err != nil {
			return mcpgo.NewToolResultError(fmt.Sprintf("embedding failed: %v", err)), nil
		}

		result := embeddings.SuggestGroups(words, vecs, k, embedClient.Model())

		// Build a natural-language interpretation of overall quality
		var qualityLabel string
		switch {
		case result.OverallQuality >= 0.5:
			qualityLabel = "strong — groups are tight and well-separated; high trust in these suggestions"
		case result.OverallQuality >= 0.2:
			qualityLabel = "moderate — reasonable separation; use alongside your own reasoning"
		default:
			qualityLabel = "weak — words may share context across groups (common with wordplay puzzles); treat as a hint only"
		}

		out := map[string]interface{}{
			"suggested_groups":      result.Groups,
			"overall_quality":       result.OverallQuality,
			"overall_quality_label": qualityLabel,
			"embedding_model":       result.EmbeddingModel,
			"words_analysed":        words,
			"guidance": "Cross-reference with tried_combinations before acting. " +
				"avg_similarity > 0.80 = tight cluster. " +
				"The word closest to min_similarity is the most likely misfit in a one_away situation. " +
				"Purple-difficulty groups often score lower here — do not dismiss low-confidence groups outright.",
		}
		data, _ := json.Marshal(out)
		return mcpgo.NewToolResultText(string(data)), nil
	})

	// ── memory_note ───────────────────────────────────────────────────────────
	s.AddTool(mcpgo.NewTool("memory_note",
		mcpgo.WithDescription(
			"Save a note that persists across turns within this run. "+
				"Use this to record anything that could improve future guesses or future games: "+
				"patterns you noticed, red herrings to watch for, words that tricked you, "+
				"category shapes that recur, or lessons from a failed attempt. "+
				"Writing the same key overwrites the previous value. "+
				"Notes are wiped at the start of every new solver run.",
		),
		mcpgo.WithString("key", mcpgo.Required(), mcpgo.Description(
			"Short identifier for this note, e.g. 'lesson_purple', 'red_herring_BANK', 'puzzle_pattern'",
		)),
		mcpgo.WithString("content", mcpgo.Required(), mcpgo.Description(
			"The note content — what you learned, what to remember, what to avoid",
		)),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()
		key, _     := args["key"].(string)
		content, _ := args["content"].(string)
		if key == "" || content == "" {
			return mcpgo.NewToolResultError("key and content required"), nil
		}
		mem.write(key, content)
		out := map[string]interface{}{
			"saved": key,
			"tip":   "Call memory_list at the start of each game to recall your notes.",
		}
		data, _ := json.Marshal(out)
		return mcpgo.NewToolResultText(string(data)), nil
	})

	// ── memory_list ───────────────────────────────────────────────────────────
	s.AddTool(mcpgo.NewTool("memory_list",
		mcpgo.WithDescription(
			"Read all saved notes from this run. "+
				"Call this at the start of every game to recall lessons from previous games. "+
				"Notes contain patterns, red herrings, and strategies you recorded with memory_note.",
		),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		notes := mem.list()
		out := map[string]interface{}{
			"notes": notes,
			"count": len(notes),
			"tip":   "Apply these lessons before guessing. Use memory_note to add new observations after each game.",
		}
		data, _ := json.Marshal(out)
		return mcpgo.NewToolResultText(string(data)), nil
	})

	// ── memory_delete ─────────────────────────────────────────────────────────
	s.AddTool(mcpgo.NewTool("memory_delete",
		mcpgo.WithDescription(
			"Delete a single note by key. "+
				"Use this to prune stale entries: solved groups that are no longer relevant, "+
				"hunches that turned out wrong, or notes from a puzzle you have now finished. "+
				"Keeping memory lean makes memory_list easier to read.",
		),
		mcpgo.WithString("key", mcpgo.Required(), mcpgo.Description("Key of the note to delete")),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()
		key, _ := args["key"].(string)
		if key == "" {
			return mcpgo.NewToolResultError("key required"), nil
		}
		found := mem.delete(key)
		out := map[string]interface{}{
			"deleted": found,
			"key":     key,
		}
		data, _ := json.Marshal(out)
		return mcpgo.NewToolResultText(string(data)), nil
	})

	// ── memory_clear ──────────────────────────────────────────────────────────
	s.AddTool(mcpgo.NewTool("memory_clear",
		mcpgo.WithDescription(
			"Wipe all saved notes. Called automatically by the solver at the start of every run. "+
				"Do not call this yourself during a run.",
		),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		n := mem.clear()
		out := map[string]interface{}{
			"cleared": n,
		}
		data, _ := json.Marshal(out)
		return mcpgo.NewToolResultText(string(data)), nil
	})

	return server.NewSSEServer(s, server.WithBaseURL(baseURL+"/mcp")), mem
}
