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
	ProcessGuess(sessionID string, words []string) (*game.GuessResult, error)
	LoadCurrentPuzzle() (*game.Puzzle, error)
	LoadMaxMistakes() int
	AdvancePuzzle()
}

// Build creates and returns the SSE HTTP handler for the MCP server.
func Build(srv gameAPI, hub *api.Hub, baseURL string) http.Handler {
	s := server.NewMCPServer("connections-game", "1.0.0",
		server.WithToolCapabilities(false),
	)

	// get_board
	s.AddTool(mcpgo.NewTool("get_board",
		mcpgo.WithDescription("Get the current board: remaining words and solved groups"),
		mcpgo.WithString("session_id", mcpgo.Required(), mcpgo.Description("Game session ID")),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()
		sessionID, _ := args["session_id"].(string)
		if sessionID == "" {
			return mcpgo.NewToolResultError("session_id required"), nil
		}
		return mcpgo.NewToolResultText(fmt.Sprintf(`{"session_id":%q,"note":"call submit_guess with 4 words from the remaining list"}`, sessionID)), nil
	})

	// get_state
	s.AddTool(mcpgo.NewTool("get_state",
		mcpgo.WithDescription("Get full game state: remaining words, solved groups, mistakes left, status"),
		mcpgo.WithString("session_id", mcpgo.Required(), mcpgo.Description("Game session ID")),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()
		sessionID, _ := args["session_id"].(string)
		if sessionID == "" {
			return mcpgo.NewToolResultError("session_id required"), nil
		}
		hub.Broadcast(sessionID, game.WSEvent{Type: "state_sync", Payload: map[string]string{"session_id": sessionID}})
		return mcpgo.NewToolResultText(fmt.Sprintf(`{"session_id":%q,"hint":"use submit_guess with 4 comma-separated words"}`, sessionID)), nil
	})

	// submit_guess
	s.AddTool(mcpgo.NewTool("submit_guess",
		mcpgo.WithDescription("Submit 4 words as a guess. Correct = group revealed. Wrong = lose a mistake."),
		mcpgo.WithString("session_id", mcpgo.Required(), mcpgo.Description("Game session ID")),
		mcpgo.WithString("words", mcpgo.Required(), mcpgo.Description("Comma-separated 4 words, e.g. JACK,SOAK,POCKET,SPINE")),
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
			words[i] = strings.TrimSpace(p)
		}

		result, err := srv.ProcessGuess(sessionID, words)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}

		hub.Broadcast(sessionID, game.WSEvent{Type: "guess_result", Payload: result})
		if result.Status == "won" || result.Status == "lost" {
			hub.Broadcast(sessionID, game.WSEvent{
				Type:    "game_complete",
				Payload: map[string]interface{}{"won": result.Status == "won"},
			})
			if result.Status == "won" {
				srv.AdvancePuzzle()
			}
		}

		data, _ := json.Marshal(result)
		return mcpgo.NewToolResultText(string(data)), nil
	})

	// new_game
	s.AddTool(mcpgo.NewTool("new_game",
		mcpgo.WithDescription("Get info about the current puzzle. Create a session via POST /api/session."),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		puzzle, err := srv.LoadCurrentPuzzle()
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText(fmt.Sprintf(
			`{"note":"create a session via POST /api/session","puzzle_id":%d,"date":%q,"word_count":16}`,
			puzzle.ID, puzzle.Date,
		)), nil
	})

	// set_max_mistakes
	s.AddTool(mcpgo.NewTool("set_max_mistakes",
		mcpgo.WithDescription("Adjust max mistakes for next session (1-10). Persists via PUT /api/config/max_mistakes."),
		mcpgo.WithNumber("count", mcpgo.Required(), mcpgo.Description("Number of allowed mistakes (1-10)")),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()
		count, _ := args["count"].(float64)
		if count < 1 || count > 10 {
			return mcpgo.NewToolResultError("count must be 1-10"), nil
		}
		return mcpgo.NewToolResultText(fmt.Sprintf(`{"max_mistakes":%d,"note":"use PUT /api/config/max_mistakes to persist"}`, int(count))), nil
	})

	return server.NewSSEServer(s, server.WithBaseURL(baseURL+"/mcp"))
}
