// solve: agent loop that plays Connections infinitely via MCP + LM Studio.
//
// Usage:
//
//	go run ./cmd/solve                               # plain logs, infinite
//	go run ./cmd/solve -tui                          # split-pane TUI with live reasoning
//	go run ./cmd/solve -rounds 5                     # stop after 5 games
//	go run ./cmd/solve -wins 3                       # stop after 3 wins
//	go run ./cmd/solve -prompt-dir ../PROMPT         # custom stage prompt directory
//	go run ./cmd/solve -base-url http://localhost:1234/v1
//
// The solver is fully stage-driven: each game event injects exactly one focused
// prompt (see prompts.go / runner.go), prefixed with a <context> reference block
// (see gamectx.go). No other context is injected into the conversation.
package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	openai "github.com/sashabaranov/go-openai"

	"connections/internal/config"
)

const (
	mcpURL   = "http://localhost:8080/mcp/sse"
	maxTurns = 80

	maxOverthinks = 3 // consecutive watchdog triggers before aborting a game
)

func main() {
	cfg := config.Load()

	baseURL := flag.String("base-url", "http://localhost:1234/v1", "OpenAI-compatible API base URL")
	model := flag.String("model", "", "Model name (default: auto-detect from /v1/models)")
	rounds := flag.Int("rounds", 0, "Games to play (0 = infinite)")
	wins := flag.Int("wins", 0, "Stop after this many wins (0 = no win cap)")
	promptDir := flag.String("prompt-dir", cfg.PromptDir, "Directory containing stage prompt files")
	csvPath := flag.String("csv", "../output/results.csv", "CSV file for game results (empty = disabled)")
	maxTokens := flag.Int("max-tokens", 0, "Max tokens per response (0 = unlimited)")
	useTUI := flag.Bool("tui", false, "Split-pane TUI with live reasoning stream")
	flag.Parse()

	wdTimeout := cfg.WatchdogTimeout
	wdTokenLimit := cfg.WatchdogTokenLimit

	ctx := context.Background()

	// Load prompts first — fail fast on a missing/empty prompt file before
	// opening any network connection.
	p, err := loadPrompts(*promptDir)
	if err != nil {
		log.Fatalf("load prompts from %q: %v", *promptDir, err)
	}

	aiCfg := openai.DefaultConfig("lm-studio")
	aiCfg.BaseURL = *baseURL
	ai := openai.NewClientWithConfig(aiCfg)

	resolvedModel, err := resolveModel(ctx, ai, *model)
	if err != nil {
		log.Fatalf("model: %v", err)
	}

	mc, err := mcpclient.NewSSEMCPClient(mcpURL)
	if err != nil {
		log.Fatalf("mcp connect: %v", err)
	}
	if err := mc.Start(ctx); err != nil {
		log.Fatalf("mcp start: %v", err)
	}
	defer mc.Close()

	if _, err := mc.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo:      mcp.Implementation{Name: "connections-solver", Version: "1.0.0"},
		},
	}); err != nil {
		log.Fatalf("mcp init: %v", err)
	}

	toolsResult, err := mc.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		log.Fatalf("list tools: %v", err)
	}
	tools := convertTools(toolsResult.Tools)

	var csvWriter *csv.Writer
	if *csvPath != "" {
		if err := os.MkdirAll(filepath.Dir(*csvPath), 0755); err != nil {
			log.Fatalf("csv mkdir: %v", err)
		}
		f, err := os.OpenFile(*csvPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatalf("csv open: %v", err)
		}
		defer f.Close()
		csvWriter = csv.NewWriter(f)
		info, _ := f.Stat()
		if info.Size() == 0 {
			csvWriter.Write([]string{"game", "timestamp", "status", "mistakes", "max_mistakes", "turns", "session_id"})
		}
	}

	if _, err := mc.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: "memory_clear"},
	}); err != nil {
		log.Printf("memory_clear: %v", err)
	} else {
		log.Printf("memory cleared")
	}

	log.Printf("model=%s server=%s tools=%d prompt-dir=%s csv=%s rounds=%d wins=%d max-tokens=%d tui=%v",
		resolvedModel, *baseURL, len(tools), *promptDir, *csvPath, *rounds, *wins, *maxTokens, *useTUI)

	// shared game loop — runs directly or in a goroutine depending on TUI flag
	gameLoop := func(sender *tuiSender) {
		stats := &sessionStats{}
		for gameNum := 1; *rounds == 0 || gameNum <= *rounds; gameNum++ {
			if *rounds > 0 {
				log.Printf("game %d/%d starting", gameNum, *rounds)
			} else {
				log.Printf("game %d starting", gameNum)
			}

			result, err := runGame(ctx, ai, mc, resolvedModel, p, tools, *maxTokens, wdTimeout, wdTokenLimit, sender, gameNum == 1)
			if err != nil {
				log.Printf("game %d error: %v", gameNum, err)
				result = gameResult{GameNum: gameNum, Status: "error", At: time.Now()}
			}
			result.GameNum = gameNum

			stats.record(result)
			printGameSummary(result, stats)

			if csvWriter != nil {
				csvWriter.Write([]string{
					strconv.Itoa(result.GameNum),
					result.At.Format(time.RFC3339),
					result.Status,
					strconv.Itoa(result.Mistakes),
					strconv.Itoa(result.MaxMistakes),
					strconv.Itoa(result.Turns),
					result.SessionID,
				})
				csvWriter.Flush()
			}

			if sender != nil {
				sender.Status(fmt.Sprintf(
					"model: %s  game: %d  W:%d L:%d  last: %s (%d/%d mistakes)  turn: %d",
					resolvedModel, gameNum, stats.Wins, stats.Losses,
					result.Status, result.Mistakes, result.MaxMistakes, result.Turns,
				))
			}

			if *wins > 0 && stats.Wins >= *wins {
				log.Printf("reached win target: %d", *wins)
				break
			}
		}
		stats.print()
		if sender != nil {
			sender.Done(nil)
		}
	}

	if *useTUI {
		events := make(chan tuiEvent, 512)
		sender := &tuiSender{ch: events}

		// redirect all log output to TUI left pane
		log.SetOutput(sender)

		go gameLoop(sender)

		initialStatus := fmt.Sprintf("model: %s  prompt-dir: %s  rounds: %d  wins: %d  max-tokens: %d",
			resolvedModel, *promptDir, *rounds, *wins, *maxTokens)
		prog := tea.NewProgram(
			newTUI(events, initialStatus),
			tea.WithAltScreen(),
		)
		if _, err := prog.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "tui error:", err)
			os.Exit(1)
		}
	} else {
		gameLoop(nil)
	}
}
