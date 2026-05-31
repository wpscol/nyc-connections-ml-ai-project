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

## Solving strategy
1. Call new_game to start (or list_sessions to find an active session).
2. Call get_state — read the remaining words carefully.
3. Look for the most obvious group first (start with Yellow-level thinking):
   - Literal sets: "___ fish", "things in a kitchen", "shades of blue"
   - Shared prefix/suffix: all can follow "OVER___" or precede "___HOUSE"
   - Famous groups: "Beatles members", "US presidents"
4. Pick the group you are MOST confident about and call submit_guess.
5. Read the result:
   - correct=true → call get_state again and repeat from step 3.
   - one_away=true → you were close; swap one word and retry.
   - correct=false, one_away=false → reconsider entirely; those 4 words likely span 2+ groups.
6. Never guess the same wrong combination twice.
7. If only 2 groups remain, the last one is forced — no need to guess it separately.

## Red-herring warning
The puzzle deliberately puts misleading words on the board. A word that "obviously" fits one group may actually belong to another. When you are unsure between two groups, solve the one you are 100% sure of first to shrink the board.

## Tool workflow
new_game → get_state → (reason) → submit_guess → (repeat) → game ends → next_game or restart_game`

// Build creates and returns the SSE HTTP handler for the MCP server.
func Build(srv gameAPI, hub *api.Hub, baseURL string) http.Handler {
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
			"Get the full current state of a game session: remaining words on the board, "+
				"already-solved groups, mistakes left, and game status. "+
				"Call this at the start of your turn and after every guess to see the updated board. "+
				"Use the remaining words to reason about which 4 share a theme — then submit ONLY that one group.",
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

		var tip string
		switch state.Status {
		case "won":
			tip = "You won! Call next_game to play the next puzzle or restart_game to replay this one."
		case "lost":
			tip = "Game over. Call restart_game to try again or next_game to move on."
		default:
			remaining := len(state.RemainingWords)
			tip = fmt.Sprintf(
				"%d words remain in %d unsolved group(s). You have %d mistake(s) left. "+
					"Scan remaining words for the most obvious shared theme, then call submit_guess.",
				remaining, remaining/4, state.MistakesLeft,
			)
		}

		out := map[string]interface{}{
			"session_id":    sessionID,
			"remaining":     state.RemainingWords,
			"solved":        state.Solved,
			"mistakes_left": state.MistakesLeft,
			"max_mistakes":  state.MaxMistakes,
			"status":        state.Status,
			"tip":           tip,
		}
		data, _ := json.Marshal(out)
		return mcpgo.NewToolResultText(string(data)), nil
	})

	// ── get_board ─────────────────────────────────────────────────────────────
	s.AddTool(mcpgo.NewTool("get_board",
		mcpgo.WithDescription(
			"Lightweight board view: remaining words and already-solved groups, without mistake counts. "+
				"Use get_state instead when you need the full picture (mistakes left, status). "+
				"Use get_board when you only need a quick look at what words are still available.",
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
		out := map[string]interface{}{
			"remaining": state.RemainingWords,
			"solved":    state.Solved,
			"tip":       "Reason about which 4 remaining words share a theme, then call submit_guess with only those 4.",
		}
		data, _ := json.Marshal(out)
		return mcpgo.NewToolResultText(string(data)), nil
	})

	// ── submit_guess ──────────────────────────────────────────────────────────
	s.AddTool(mcpgo.NewTool("submit_guess",
		mcpgo.WithDescription(
			"Submit exactly 4 words as a single group guess. "+
				"Words must come from the current remaining board (check get_state first). "+
				"Result interpretation: "+
				"correct=true → group solved, category revealed, words removed from board; "+
				"one_away=true → 3 of your 4 words were right, swap one word and retry; "+
				"correct=false, one_away=false → wrong combination, reconsider all 4 words. "+
				"RULE: submit ONLY ONE group per call. Always call get_state after to see the updated board before guessing again.",
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

	return server.NewSSEServer(s, server.WithBaseURL(baseURL+"/mcp"))
}
