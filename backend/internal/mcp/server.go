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
	ProcessGuess(sessionID string, words []string, source string) (*game.GuessResult, error)
	GetSession(sessionID string) (*game.GameState, error)
	LoadCurrentPuzzle() (*game.Puzzle, error)
	LoadMaxMistakes() int
	BroadcastIfComplete(sessionID string)
	RestartSession(sessionID string) (*game.GameState, error)
	NextSession(sessionID string) (*game.GameState, error)
	PrevSession(sessionID string) (*game.GameState, error)
}

// Build creates and returns the SSE HTTP handler for the MCP server.
func Build(srv gameAPI, hub *api.Hub, baseURL string) http.Handler {
	s := server.NewMCPServer(
		"connections-game",
		"1.0.0",
		server.WithToolCapabilities(false),
		server.WithInstructions(
			"You are playing NYT Connections. The board has 16 words forming 4 secret groups of 4.\n"+
				"IMPORTANT: Solve ONE group at a time. Call get_state to see remaining words, "+
				"reason carefully about which 4 belong together (theme/category), then submit ONLY those 4 via submit_guess. "+
				"Wait for the result before attempting another group. Do not submit multiple groups in sequence without checking state first.",
		),
	)

	// get_board — returns remaining words and solved groups
	s.AddTool(mcpgo.NewTool("get_board",
		mcpgo.WithDescription(
			"Get the current board: shuffled remaining words and already-solved groups. "+
				"Use this to see what words are left. Then reason about ONE group and call submit_guess for just that group.",
		),
		mcpgo.WithString("session_id", mcpgo.Required(), mcpgo.Description("Game session ID")),
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
			"remaining":  state.RemainingWords,
			"solved":     state.Solved,
			"next_step":  "reason about which 4 remaining words share a theme, then submit ONLY that one group",
		}
		data, _ := json.Marshal(out)
		return mcpgo.NewToolResultText(string(data)), nil
	})

	// get_state — returns full game state
	s.AddTool(mcpgo.NewTool("get_state",
		mcpgo.WithDescription(
			"Get full game state: remaining words, solved groups, mistakes left, and status. "+
				"After reading the state, identify ONE group of 4 that clearly belong together and submit only that group.",
		),
		mcpgo.WithString("session_id", mcpgo.Required(), mcpgo.Description("Game session ID")),
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
		out := map[string]interface{}{
			"remaining":     state.RemainingWords,
			"solved":        state.Solved,
			"mistakes_left": state.MistakesLeft,
			"max_mistakes":  state.MaxMistakes,
			"status":        state.Status,
			"instruction":   "Pick ONE group of 4 words that clearly share a theme. Submit only those 4 words. Do not try to solve multiple groups at once.",
		}
		data, _ := json.Marshal(out)
		return mcpgo.NewToolResultText(string(data)), nil
	})

	// submit_guess — one group at a time
	s.AddTool(mcpgo.NewTool("submit_guess",
		mcpgo.WithDescription(
			"Submit exactly 4 words as ONE group guess. Correct = the group is revealed and removed from the board. "+
				"Wrong = you lose one mistake. IMPORTANT: submit only ONE group per call. "+
				"After seeing the result, call get_state again before attempting the next group.",
		),
		mcpgo.WithString("session_id", mcpgo.Required(), mcpgo.Description("Game session ID")),
		mcpgo.WithString("words", mcpgo.Required(), mcpgo.Description("Exactly 4 comma-separated words from the remaining board, e.g. PUMP,BOOT,MILE,SNEAKER")),
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

		data, _ := json.Marshal(result)
		return mcpgo.NewToolResultText(string(data)), nil
	})

	// restart_game — reset the current session to replay the same puzzle
	s.AddTool(mcpgo.NewTool("restart_game",
		mcpgo.WithDescription("Restart the current session: reshuffle the same puzzle, clear solved groups and mistakes. The UI updates live."),
		mcpgo.WithString("session_id", mcpgo.Required(), mcpgo.Description("Game session ID")),
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
		out := map[string]interface{}{"status": state.Status, "remaining": state.RemainingWords, "note": "puzzle restarted"}
		data, _ := json.Marshal(out)
		return mcpgo.NewToolResultText(string(data)), nil
	})

	// next_game — advance the current session to the next puzzle
	s.AddTool(mcpgo.NewTool("next_game",
		mcpgo.WithDescription("Advance the current session to the next puzzle. Use after finishing a puzzle. The UI updates live."),
		mcpgo.WithString("session_id", mcpgo.Required(), mcpgo.Description("Game session ID")),
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
		out := map[string]interface{}{"status": state.Status, "remaining": state.RemainingWords, "puzzle_id": state.PuzzleID, "note": "advanced to next puzzle"}
		data, _ := json.Marshal(out)
		return mcpgo.NewToolResultText(string(data)), nil
	})

	// prev_game — rewind the current session to the previous puzzle
	s.AddTool(mcpgo.NewTool("prev_game",
		mcpgo.WithDescription("Go back to the previous puzzle in the current session. The UI updates live."),
		mcpgo.WithString("session_id", mcpgo.Required(), mcpgo.Description("Game session ID")),
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
		out := map[string]interface{}{"status": state.Status, "remaining": state.RemainingWords, "puzzle_id": state.PuzzleID, "note": "went back to previous puzzle"}
		data, _ := json.Marshal(out)
		return mcpgo.NewToolResultText(string(data)), nil
	})

	// new_game — get current puzzle info + session creation hint
	s.AddTool(mcpgo.NewTool("new_game",
		mcpgo.WithDescription("Get info about the current puzzle and create a new session. Returns session_id and the 16 shuffled words."),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		puzzle, err := srv.LoadCurrentPuzzle()
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}

		// Collect all words from categories
		var words []string
		for _, cat := range puzzle.Categories {
			for _, card := range cat.Cards {
				words = append(words, card.Content)
			}
		}

		out := map[string]interface{}{
			"puzzle_id":   puzzle.ID,
			"date":        puzzle.Date,
			"words":       words,
			"instruction": "Call POST /api/session to create a session, then use get_state to see the board and solve ONE group at a time.",
		}
		data, _ := json.Marshal(out)
		return mcpgo.NewToolResultText(string(data)), nil
	})

	// set_max_mistakes
	s.AddTool(mcpgo.NewTool("set_max_mistakes",
		mcpgo.WithDescription("Adjust max allowed mistakes for the next session (1-10)."),
		mcpgo.WithNumber("count", mcpgo.Required(), mcpgo.Description("Number of allowed mistakes (1-10)")),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()
		count, _ := args["count"].(float64)
		if count < 1 || count > 10 {
			return mcpgo.NewToolResultError("count must be 1-10"), nil
		}
		return mcpgo.NewToolResultText(fmt.Sprintf(`{"max_mistakes":%d}`, int(count))), nil
	})

	return server.NewSSEServer(s, server.WithBaseURL(baseURL+"/mcp"))
}
