package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"connections/internal/api"
	"connections/internal/embeddings"
	"connections/internal/game"
)

type gameAPI interface {
	CreateSession() (id string, state *game.GameState, date string, err error)
	ProcessGuess(sessionID string, words []string, source string) (*game.GuessResult, error)
	GetSession(sessionID string) (*game.GameState, error)
	LoadCurrentPuzzle() (*game.Puzzle, error)
	LoadMaxMistakes() int
	SetMaxMistakes(count int) error
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
1. Call new_game to start (or list_sessions to find an active session).
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
new_game → get_state → (check tried_combinations) → (reason, optionally suggest_groups) → submit_guess → (repeat) → game ends → next_game or restart_game`

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

// Build creates and returns the SSE HTTP handler for the MCP server.
// embedClient may be nil — the suggest_groups tool will report unavailable in that case.
func Build(srv gameAPI, hub *api.Hub, baseURL string, embedClient *embeddings.Client) http.Handler {
	s := server.NewMCPServer(
		"connections-game",
		"1.0.0",
		server.WithToolCapabilities(false),
		server.WithInstructions(serverInstructions),
	)

	// ── new_game ─────────────────────────────────────────────────────────────
	s.AddTool(mcpgo.NewTool("new_game",
		mcpgo.WithDescription(
			"Start a fresh game. Creates a new session for the current puzzle and returns the "+
				"session_id you will pass to every other tool, plus the 16 shuffled board words. "+
				"Call this first if you have no session yet, or after finishing a game and wanting to play again. "+
				"If a browser is already open on the same puzzle, prefer list_sessions to share that session "+
				"so the human can watch you play in real time.",
		),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		id, state, date, err := srv.CreateSession()
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		out := map[string]interface{}{
			"session_id":    id,
			"date":          date,
			"puzzle_id":     state.PuzzleID,
			"remaining":     state.RemainingWords,
			"max_mistakes":  state.MaxMistakes,
			"status":        state.Status,
			"next_step":     "Call get_state with this session_id to see the board and start solving. Remember: solve ONE group at a time.",
		}
		data, _ := json.Marshal(out)
		return mcpgo.NewToolResultText(string(data)), nil
	})

	// ── list_sessions ─────────────────────────────────────────────────────────
	s.AddTool(mcpgo.NewTool("list_sessions",
		mcpgo.WithDescription(
			"List all existing game sessions and their status. "+
				"ws_active=true means a human browser is currently connected to that session — "+
				"use that session_id so the human can watch your moves in real time. "+
				"status values: 'playing' (still going), 'won', 'lost'. "+
				"Call this when you want to join or observe an in-progress game instead of starting a new one.",
		),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		sessions, err := srv.ListSessions()
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		out := map[string]interface{}{
			"sessions":  sessions,
			"tip":       "Pick a session with ws_active=true and status='playing' to solve while the human watches. Then call get_state with that session_id.",
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

		tried := formatTriedCombinations(state.Guesses)

		var tip string
		switch state.Status {
		case "won":
			tip = "You won! Call next_game to play the next puzzle or restart_game to replay this one."
		case "lost":
			tip = "Game over. Call restart_game to try again or next_game to move on."
		default:
			remaining := len(state.RemainingWords)
			wrongCount := 0
			for _, g := range state.Guesses {
				if !g.Correct {
					wrongCount++
				}
			}
			tip = fmt.Sprintf(
				"%d words remain in %d unsolved group(s). %d mistake(s) left. "+
					"%d prior attempt(s) in tried_combinations — check it before guessing to avoid duplicates.",
				remaining, remaining/4, state.MistakesLeft, len(tried),
			)
		}

		out := map[string]interface{}{
			"session_id":          sessionID,
			"remaining":           state.RemainingWords,
			"solved":              state.Solved,
			"mistakes_left":       state.MistakesLeft,
			"max_mistakes":        state.MaxMistakes,
			"status":              state.Status,
			"tried_combinations":  tried,
			"tip":                 tip,
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
		tried := formatTriedCombinations(state.Guesses)
		out := map[string]interface{}{
			"remaining":          state.RemainingWords,
			"solved":             state.Solved,
			"tried_combinations": tried,
			"tip":                "Check tried_combinations first, then reason about which 4 remaining words share a theme, then call submit_guess.",
		}
		data, _ := json.Marshal(out)
		return mcpgo.NewToolResultText(string(data)), nil
	})

	// ── submit_guess ──────────────────────────────────────────────────────────
	s.AddTool(mcpgo.NewTool("submit_guess",
		mcpgo.WithDescription(
			"Submit exactly 4 words as a single group guess. "+
				"PRE-FLIGHT CHECK (mandatory): call get_state first and verify your 4 words do NOT "+
				"match any entry in tried_combinations (order-independent set comparison). "+
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
			nextStep = "Puzzle solved! Call next_game to advance or restart_game to replay."
		case result.Status == "lost":
			nextStep = "No mistakes remaining — game over. Call restart_game to try again."
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
			"Reset the session to replay the same puzzle from scratch: clears solved groups, "+
				"restores all 16 words, and resets mistakes to the configured maximum. "+
				"Use this after losing, or to try a different solving strategy on the same puzzle. "+
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
			"note":         "Puzzle reset. Call get_state to see the fresh board and start solving.",
		}
		data, _ := json.Marshal(out)
		return mcpgo.NewToolResultText(string(data)), nil
	})

	// ── next_game ─────────────────────────────────────────────────────────────
	s.AddTool(mcpgo.NewTool("next_game",
		mcpgo.WithDescription(
			"Advance the session to the next puzzle in the archive. "+
				"Use after finishing (won or lost) to keep playing. "+
				"Updates the global current-puzzle pointer so new sessions also start on this puzzle. "+
				"The browser UI updates live.",
		),
		mcpgo.WithString("session_id", mcpgo.Required(), mcpgo.Description("Session ID to advance")),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()
		sessionID, _ := args["session_id"].(string)
		if sessionID == "" {
			return mcpgo.NewToolResultError("session_id required"), nil
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
				"Useful for revisiting a puzzle you want to solve differently. "+
				"Updates the global current-puzzle pointer. The browser UI updates live.",
		),
		mcpgo.WithString("session_id", mcpgo.Required(), mcpgo.Description("Session ID to rewind")),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()
		sessionID, _ := args["session_id"].(string)
		if sessionID == "" {
			return mcpgo.NewToolResultError("session_id required"), nil
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

	// ── set_max_mistakes ──────────────────────────────────────────────────────
	s.AddTool(mcpgo.NewTool("set_max_mistakes",
		mcpgo.WithDescription(
			"Change the number of allowed wrong guesses for all future sessions (1–10, default 4). "+
				"Higher = more forgiving; lower = harder challenge. "+
				"Takes effect immediately for new sessions and notifies all connected browsers. "+
				"Does NOT change the mistake count for already-running sessions.",
		),
		mcpgo.WithNumber("count", mcpgo.Required(), mcpgo.Description("Allowed mistakes per game, between 1 and 10")),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()
		count, _ := args["count"].(float64)
		if count < 1 || count > 10 {
			return mcpgo.NewToolResultError("count must be between 1 and 10"), nil
		}
		if err := srv.SetMaxMistakes(int(count)); err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		out := map[string]interface{}{
			"max_mistakes": int(count),
			"note":         fmt.Sprintf("New sessions will allow %d mistake(s). Call new_game to start one.", int(count)),
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

	return server.NewSSEServer(s, server.WithBaseURL(baseURL+"/mcp"))
}
