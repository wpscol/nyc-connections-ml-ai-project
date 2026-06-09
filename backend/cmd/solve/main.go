// solve: agent loop that plays Connections infinitely via MCP + LM Studio.
//
// Usage:
//
//	go run ./cmd/solve                               # plain logs, infinite
//	go run ./cmd/solve -tui                          # split-pane TUI with live reasoning
//	go run ./cmd/solve -rounds 5                     # stop after 5 games
//	go run ./cmd/solve -prompt-dir ../PROMPT         # custom stage prompt directory
//	go run ./cmd/solve -base-url http://localhost:1234/v1
package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	openai "github.com/sashabaranov/go-openai"
)

const (
	mcpURL        = "http://localhost:8080/mcp/sse"
	maxTurns      = 80
	reminderEvery = 30 // rules reminder (generic)
	progressEvery = 8  // game-state context injection (specific)

	watchdogTimeout    = 3 * time.Minute
	watchdogTokenLimit = 8_000 // approximate content tokens before interrupt
	maxOverthinks      = 3     // consecutive watchdog triggers before aborting a game

	repeatTailLen   = 400 // chars of streamed output kept for the periodicity check
	repeatWindow    = 160 // tail length that must be strictly periodic to trip
	repeatMaxPeriod = 40  // longest repeating unit we treat as a degenerate loop
)

// ── watchdog ──────────────────────────────────────────────────────────────────

// watchdog monitors a single model call and cancels its context if the model
// exceeds the token budget or takes longer than watchdogTimeout.
// Set fired=true BEFORE calling cancel so stop() always sees the right value.
type watchdog struct {
	ctx    context.Context
	cancel context.CancelFunc
	tokens chan int // buffered; send approximate token counts here
	mu     sync.Mutex
	fired  bool
	reason string
}

func newWatchdog(parent context.Context) *watchdog {
	ctx, cancel := context.WithCancel(parent)
	w := &watchdog{
		ctx:    ctx,
		cancel: cancel,
		tokens: make(chan int, 512),
	}
	go func() {
		defer cancel()
		timer := time.NewTimer(watchdogTimeout)
		defer timer.Stop()
		total := 0
		for {
			select {
			case n := <-w.tokens:
				total += n
				if total >= watchdogTokenLimit {
					w.setFired(fmt.Sprintf("token budget exceeded (~%d tokens)", total))
					return
				}
			case <-timer.C:
				w.setFired(fmt.Sprintf("no response in %.0fs", watchdogTimeout.Seconds()))
				return
			case <-parent.Done():
				return // outer context canceled — not our fault
			}
		}
	}()
	return w
}

func (w *watchdog) setFired(reason string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.fired {
		w.fired = true
		w.reason = reason
	}
}

// stop cancels the watchdog and returns (fired, reason).
// Always call this after complete() returns to release resources.
func (w *watchdog) stop() (bool, string) {
	w.cancel()
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.fired, w.reason
}

// addTokens sends an approximate token count to the monitor (non-blocking).
func (w *watchdog) addTokens(n int) {
	select {
	case w.tokens <- n:
	default:
	}
}

// looksRepetitive reports whether the last repeatWindow chars of s are strictly
// periodic with a unit no longer than repeatMaxPeriod — the signature of a
// degenerate generation loop ("000,000,..." or "<|channel|><|channel|>...").
// Token boundaries don't matter: it works on the accumulated character stream.
func looksRepetitive(s string) bool {
	if len(s) < repeatWindow {
		return false
	}
	tail := s[len(s)-repeatWindow:]
	for p := 1; p <= repeatMaxPeriod; p++ {
		periodic := true
		for i := p; i < len(tail); i++ {
			if tail[i] != tail[i-p] {
				periodic = false
				break
			}
		}
		if periodic {
			return true
		}
	}
	return false
}

const contextReminder = `[TURN %d — RULES REMINDER]
• No session creation — list_sessions only. Join ws_active=true.
• Never submit a set already in tried_combinations (order-independent: A,B,C,D == D,C,B,A).
• After correct group → memory_note(key="solved_N", content="W1,W2,W3,W4=TITLE") immediately.
• After restart → memory_list → re-submit all solved_N groups first, then analyse.
• LOSS → restart_game (never skip). WIN → memory_note lessons → memory_delete puzzle-specific keys → next_game.
• Use memory_list when stuck mid-game — your notes from earlier rounds may hold the answer.
Re-read your system instructions if any rule is unclear.`

// ── game progress tracker ─────────────────────────────────────────────────────

type guessRecord struct {
	words []string
	cat   string // category title; non-empty only for correct guesses
}

// gameCtx tracks factual state accumulated during a single game run.
// It is injected as a compact user message after every submit_guess and every
// progressEvery turns so the model always has an accurate, scannable summary
// of what has happened — preventing it from replaying failed guesses.
type gameCtx struct {
	sessionID    string
	mistakesLeft int
	maxMistakes  int
	solved       []guessRecord
	oneAway      []guessRecord
	wrong        []guessRecord
	turn         int
}

func (g *gameCtx) hasData() bool {
	return len(g.solved) > 0 || len(g.wrong) > 0 || len(g.oneAway) > 0 || g.maxMistakes > 0
}

// reset clears per-game attempt data while keeping session identity.
// Called on restart_game (same session, fresh board).
func (g *gameCtx) reset() {
	g.solved = nil
	g.oneAway = nil
	g.wrong = nil
	g.mistakesLeft = 0
	g.maxMistakes = 0
}

func (g *gameCtx) update(toolName string, parsed map[string]interface{}, submittedWords []string) {
	if sid, ok := parsed["session_id"].(string); ok && sid != "" {
		g.sessionID = sid
	}
	if ml, ok := parsed["mistakes_left"].(float64); ok {
		g.mistakesLeft = int(ml)
	}
	if mm, ok := parsed["max_mistakes"].(float64); ok && mm > 0 {
		g.maxMistakes = int(mm)
	}
	if toolName != "submit_guess" || parsed == nil {
		return
	}
	correct, _ := parsed["correct"].(bool)
	oneAway, _ := parsed["one_away"].(bool)
	rec := guessRecord{words: submittedWords}
	switch {
	case correct:
		if cat, ok := parsed["category"].(map[string]interface{}); ok {
			rec.cat, _ = cat["title"].(string)
		}
		g.solved = append(g.solved, rec)
	case oneAway:
		g.oneAway = append(g.oneAway, rec)
	default:
		g.wrong = append(g.wrong, rec)
	}
}

func (g *gameCtx) summary() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "[GAME CONTEXT — turn %d", g.turn)
	if g.sessionID != "" {
		fmt.Fprintf(&sb, " | session %.8s", g.sessionID)
	}
	if g.maxMistakes > 0 {
		fmt.Fprintf(&sb, " | mistakes %d/%d", g.mistakesLeft, g.maxMistakes)
	}
	sb.WriteString("]\n")

	for _, s := range g.solved {
		fmt.Fprintf(&sb, "✓ %s = %s\n", strings.Join(s.words, ","), s.cat)
	}
	for _, o := range g.oneAway {
		fmt.Fprintf(&sb, "~ %s  ← one-away: 3 correct, 1 wrong — swap one word\n", strings.Join(o.words, ","))
	}
	for _, w := range g.wrong {
		fmt.Fprintf(&sb, "✗ %s  ← entirely wrong, never resubmit any permutation\n", strings.Join(w.words, ","))
	}

	wordsLeft := 16 - len(g.solved)*4
	if wordsLeft >= 0 {
		fmt.Fprintf(&sb, "%d word(s) remain in %d unsolved group(s).", wordsLeft, wordsLeft/4)
	}
	return strings.TrimRight(sb.String(), "\n")
}

// ── stage prompts ─────────────────────────────────────────────────────────────

type stagePrompts struct {
	System       string
	SessionStart string
	Analysis     string
	Correct      string
	OneAway      string
	Wrong        string
	Won          string
	Lost         string
	Restart      string
}

func loadStagePrompts(dir string) (stagePrompts, error) {
	load := func(name string) (string, error) {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return "", fmt.Errorf("%s: %w", name, err)
		}
		return strings.TrimSpace(string(data)), nil
	}
	var p stagePrompts
	var err error
	if p.System, err = load("system.md"); err != nil {
		return p, err
	}
	if p.SessionStart, err = load("session_start.md"); err != nil {
		return p, err
	}
	if p.Analysis, err = load("analysis.md"); err != nil {
		return p, err
	}
	if p.Correct, err = load("result_correct.md"); err != nil {
		return p, err
	}
	if p.OneAway, err = load("result_one_away.md"); err != nil {
		return p, err
	}
	if p.Wrong, err = load("result_wrong.md"); err != nil {
		return p, err
	}
	if p.Won, err = load("game_won.md"); err != nil {
		return p, err
	}
	if p.Lost, err = load("game_lost.md"); err != nil {
		return p, err
	}
	if p.Restart, err = load("restart.md"); err != nil {
		return p, err
	}
	return p, nil
}

// ── result types ─────────────────────────────────────────────────────────────

type gameResult struct {
	GameNum     int
	Status      string
	Mistakes    int
	MaxMistakes int
	Turns       int
	SessionID   string
	At          time.Time
}

type sessionStats struct {
	Games         int
	Wins          int
	Losses        int
	TotalMistakes int
}

func (s *sessionStats) record(r gameResult) {
	s.Games++
	if r.Status == "won" {
		s.Wins++
	} else {
		s.Losses++
	}
	s.TotalMistakes += r.Mistakes
}

func (s *sessionStats) print() {
	if s.Games == 0 {
		return
	}
	log.Printf("final stats: games=%d wins=%d losses=%d win_pct=%d total_mistakes=%d avg_mistakes=%.2f",
		s.Games, s.Wins, s.Losses, 100*s.Wins/s.Games, s.TotalMistakes,
		float64(s.TotalMistakes)/float64(s.Games))
}

// ── main ──────────────────────────────────────────────────────────────────────

func main() {
	baseURL := flag.String("base-url", "http://localhost:1234/v1", "OpenAI-compatible API base URL")
	model := flag.String("model", "", "Model name (default: auto-detect from /v1/models)")
	rounds := flag.Int("rounds", 0, "Games to play (0 = infinite)")
	promptDir := flag.String("prompt-dir", "../PROMPT", "Directory containing stage prompt files")
	csvPath := flag.String("csv", "../output/results.csv", "CSV file for game results (empty = disabled)")
	maxTokens := flag.Int("max-tokens", 0, "Max tokens per response (0 = unlimited)")
	useTUI := flag.Bool("tui", false, "Split-pane TUI with live reasoning stream")
	flag.Parse()

	ctx := context.Background()

	cfg := openai.DefaultConfig("lm-studio")
	cfg.BaseURL = *baseURL
	ai := openai.NewClientWithConfig(cfg)

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

	prompts, err := loadStagePrompts(*promptDir)
	if err != nil {
		log.Fatalf("load prompts from %q: %v", *promptDir, err)
	}

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

	log.Printf("model=%s server=%s tools=%d prompt-dir=%s csv=%s rounds=%d max-tokens=%d tui=%v",
		resolvedModel, *baseURL, len(tools), *promptDir, *csvPath, *rounds, *maxTokens, *useTUI)

	// shared game loop — runs directly or in a goroutine depending on TUI flag
	gameLoop := func(sender *tuiSender) {
		stats := &sessionStats{}
		for gameNum := 1; *rounds == 0 || gameNum <= *rounds; gameNum++ {
			if *rounds > 0 {
				log.Printf("game %d/%d starting", gameNum, *rounds)
			} else {
				log.Printf("game %d starting", gameNum)
			}

			result, err := runGame(ctx, ai, mc, resolvedModel, prompts, tools, *maxTokens, sender)
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

		initialStatus := fmt.Sprintf("model: %s  prompt-dir: %s  rounds: %d  max-tokens: %d",
			resolvedModel, *promptDir, *rounds, *maxTokens)
		p := tea.NewProgram(
			newTUI(events, initialStatus),
			tea.WithAltScreen(),
		)
		if _, err := p.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "tui error:", err)
			os.Exit(1)
		}
	} else {
		gameLoop(nil)
	}
}

// ── game logic ────────────────────────────────────────────────────────────────

const (
	priNone     = 0
	priAnalysis = 1
	priRestart  = 2
	priResult   = 3
)

func runGame(
	ctx context.Context,
	ai *openai.Client,
	mc *mcpclient.Client,
	model string,
	prompts stagePrompts,
	tools []openai.Tool,
	maxTokens int,
	sender *tuiSender,
) (gameResult, error) {
	result := gameResult{At: time.Now()}
	noToolStreak := 0
	overthinksInRow := 0

	pendingStage := ""
	pendingPri := priNone
	pendingContext := "" // compact game-state summary, injected before the stage prompt

	setStage := func(content string, pri int) {
		if pri >= pendingPri {
			pendingStage = content
			pendingPri = pri
		}
	}

	gctx := &gameCtx{}

	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: prompts.System},
		{Role: openai.ChatMessageRoleUser, Content: prompts.SessionStart},
	}

	inject := func(role, content string) {
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    role,
			Content: content,
		})
	}

	for turn := 0; turn < maxTurns; turn++ {
		gctx.turn = turn + 1

		// periodic game-state snapshot (facts, not rules)
		if turn > 0 && turn%progressEvery == 0 && gctx.hasData() {
			inject(openai.ChatMessageRoleUser, gctx.summary())
			log.Printf("progress context injected at turn %d", turn)
		}

		// periodic rules reminder
		if turn > 0 && turn%reminderEvery == 0 {
			inject(openai.ChatMessageRoleUser, fmt.Sprintf(contextReminder, turn))
			log.Printf("context reminder injected at turn %d", turn)
		}

		// context from last submit_guess (most recent game-state change)
		if pendingContext != "" {
			inject(openai.ChatMessageRoleUser, pendingContext)
			pendingContext = ""
		}

		// stage prompt for what to do next
		if pendingStage != "" {
			inject(openai.ChatMessageRoleUser, pendingStage)
			log.Printf("stage injected (pri=%d)", pendingPri)
			pendingStage = ""
			pendingPri = priNone
		}

		log.Printf("turn %d", turn+1)

		// "auto" lets the model emit reasoning text before choosing a tool (needed
		// for the TUI thinking pane). "required" is used in plain-log mode to keep
		// models that over-reason from generating walls of text with no tool call.
		toolChoice := "required"
		if sender != nil {
			toolChoice = "auto"
		}
		req := openai.ChatCompletionRequest{
			Model:      model,
			Messages:   messages,
			Tools:      tools,
			ToolChoice: toolChoice,
		}
		if maxTokens > 0 {
			req.MaxTokens = maxTokens
		}

		wd := newWatchdog(ctx)
		msg, err := complete(ctx, ai, req, sender, wd)
		fired, wdReason := wd.stop()

		if fired || (err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))) {
			// watchdog interrupted the model — it was overthinking
			overthinksInRow++
			log.Printf("watchdog fired on turn %d: %s (strike %d/%d)", turn+1, wdReason, overthinksInRow, maxOverthinks)
			if overthinksInRow >= maxOverthinks {
				return result, fmt.Errorf("model stuck in overthinking loop (%d consecutive watchdog triggers)", overthinksInRow)
			}
			nudge := fmt.Sprintf(
				"[WATCHDOG — %s]\n"+
					"You have been reasoning for too long without acting. Stop.\n\n"+
					"What you know so far:\n%s\n\n"+
					"Rules:\n"+
					"• Call a tool right now — do not continue the current reasoning chain.\n"+
					"• If you know a group with ≥80%% confidence: submit it.\n"+
					"• If stuck: call get_tried_combinations, then suggest_groups, then commit to the best option.\n"+
					"• Do not re-analyse words you have already tried.",
				wdReason,
				gctx.summary(),
			)
			inject(openai.ChatMessageRoleUser, nudge)
			if sender != nil {
				sender.ThinkDone() // dim the partial thinking in the TUI
			}
			continue
		}
		if err != nil {
			return result, fmt.Errorf("turn %d: %w", turn, err)
		}
		overthinksInRow = 0 // successful response resets the counter

		messages = append(messages, msg)
		result.Turns = turn + 1

		if strings.TrimSpace(msg.Content) != "" {
			log.Printf("model: %s", strings.TrimSpace(msg.Content))
		}

		if len(msg.ToolCalls) == 0 {
			noToolStreak++
			if noToolStreak >= 3 {
				log.Printf("no tool calls for %d turns in a row — model stuck", noToolStreak)
				return result, fmt.Errorf("model stuck: %d consecutive turns with no tool calls", noToolStreak)
			}
			messages = append(messages, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleUser,
				Content: "Stop reasoning. Call a tool now. Start with list_sessions.",
			})
			log.Printf("no tool call on turn %d, nudging (streak=%d)", turn+1, noToolStreak)
			continue
		}
		noToolStreak = 0

		log.Printf("%d tool call(s)", len(msg.ToolCalls))

		for _, tc := range msg.ToolCalls {
			// extract submitted words before the call so we can track the guess
			var submittedWords []string
			if tc.Function.Name == "submit_guess" {
				var args map[string]interface{}
				if json.Unmarshal([]byte(tc.Function.Arguments), &args) == nil {
					if ws, ok := args["words"].(string); ok {
						for _, p := range strings.Split(ws, ",") {
							submittedWords = append(submittedWords, strings.TrimSpace(strings.ToUpper(p)))
						}
					}
				}
			}

			toolResult, parsed := callTool(ctx, mc, tc)
			messages = append(messages, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				ToolCallID: tc.ID,
				Content:    toolResult,
			})

			// update game context from tool result
			gctx.update(tc.Function.Name, parsed, submittedWords)

			if sid, ok := parsed["session_id"].(string); ok && sid != "" {
				result.SessionID = sid
			}
			if status, ok := parsed["status"].(string); ok && status != "" {
				result.Status = status
			}
			if ml, ok := parsed["mistakes_left"].(float64); ok {
				if mm, ok2 := parsed["max_mistakes"].(float64); ok2 {
					result.Mistakes = int(mm) - int(ml)
					result.MaxMistakes = int(mm)
				}
			}

			switch tc.Function.Name {
			case "get_tried_combinations":
				setStage(prompts.Analysis, priAnalysis)

			case "restart_game":
				gctx.reset() // fresh board; model replays solved_N from memory
				setStage(prompts.Restart, priRestart)

			case "next_game":
				gctx = &gameCtx{} // new puzzle: wipe everything

			case "submit_guess":
				// always inject current game state before next model call
				pendingContext = gctx.summary()
				correct, _ := parsed["correct"].(bool)
				oneAway, _ := parsed["one_away"].(bool)
				switch {
				case result.Status == "won":
					setStage(prompts.Won, priResult)
				case result.Status == "lost":
					setStage(prompts.Lost, priResult)
				case correct:
					setStage(prompts.Correct, priResult)
				case oneAway:
					setStage(prompts.OneAway, priResult)
				default:
					setStage(prompts.Wrong, priResult)
				}
			}

			if result.Status == "won" || result.Status == "lost" {
				// inject terminal stage immediately so model can write memory notes
				if pendingContext != "" {
					inject(openai.ChatMessageRoleUser, pendingContext)
				}
				if pendingStage != "" {
					inject(openai.ChatMessageRoleUser, pendingStage)
					log.Printf("terminal stage injected (status=%s)", result.Status)
				}
				return result, nil
			}
		}
	}

	if result.Status == "" {
		result.Status = "error"
	}
	return result, nil
}

// ── streaming / non-streaming completion ─────────────────────────────────────

type toolCallDelta struct {
	id   string
	name string
	args strings.Builder
}

// complete calls the model. When sender != nil it streams, routing content tokens
// to the TUI right pane and assembling tool calls from stream deltas.
// wd monitors the call and cancels it on token/time overrun; pass nil to skip.
func complete(
	ctx context.Context,
	ai *openai.Client,
	req openai.ChatCompletionRequest,
	sender *tuiSender,
	wd *watchdog,
) (openai.ChatCompletionMessage, error) {
	// use watchdog context if available so it can interrupt the call
	callCtx := ctx
	if wd != nil {
		callCtx = wd.ctx
	}

	if sender == nil {
		resp, err := ai.CreateChatCompletion(callCtx, req)
		if err != nil {
			return openai.ChatCompletionMessage{}, err
		}
		return resp.Choices[0].Message, nil
	}

	// streaming path
	stream, err := ai.CreateChatCompletionStream(callCtx, req)
	if err != nil {
		return openai.ChatCompletionMessage{}, err
	}
	defer stream.Close()

	var content strings.Builder
	accum := map[int]*toolCallDelta{}

	// repetition guard: small/quantized models sometimes fall into a degenerate
	// loop emitting the same short pattern forever ("<|channel|>", "000,000,...").
	// Token boundaries vary, so comparing whole chunks is unreliable — instead we
	// accumulate a rolling tail and check whether it has become strictly periodic.
	streamTail := ""
	chunkCount := 0
	tripRepeat := func(s string) bool {
		if s == "" {
			return false
		}
		streamTail += s
		if len(streamTail) > repeatTailLen {
			streamTail = streamTail[len(streamTail)-repeatTailLen:]
		}
		chunkCount++
		if chunkCount%4 != 0 { // throttle: check every 4th chunk
			return false
		}
		return looksRepetitive(streamTail)
	}

	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// context canceled by watchdog or outer caller — propagate as-is
			return openai.ChatCompletionMessage{}, fmt.Errorf("stream: %w", err)
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta

		// reasoning_content: separate channel many local models (Gemma, QwQ, R1)
		// use for chain-of-thought. Stream it to the pane but DON'T add to the
		// assistant message — it must not be fed back as conversation history.
		if delta.ReasoningContent != "" {
			sender.Think(delta.ReasoningContent)
			if wd != nil {
				wd.addTokens(len(delta.ReasoningContent)/4 + 1)
			}
			if wd != nil && tripRepeat(delta.ReasoningContent) {
				wd.setFired("repetition loop detected (degenerate output)")
				wd.cancel()
			}
		}

		if delta.Content != "" {
			sender.Think(delta.Content)
			content.WriteString(delta.Content)
			// approximate tokens: 1 per 4 chars; watchdog uses this to enforce budget
			if wd != nil {
				wd.addTokens(len(delta.Content)/4 + 1)
			}
			if wd != nil && tripRepeat(delta.Content) {
				wd.setFired("repetition loop detected (degenerate output)")
				wd.cancel()
			}
		}

		for _, tc := range delta.ToolCalls {
			idx := 0
			if tc.Index != nil {
				idx = *tc.Index
			}
			if _, ok := accum[idx]; !ok {
				accum[idx] = &toolCallDelta{}
			}
			a := accum[idx]
			if tc.ID != "" {
				a.id = tc.ID
			}
			if tc.Function.Name != "" {
				a.name = tc.Function.Name
				// show which tool the model chose — useful when it emits no content
				sender.Think("\n[→ " + tc.Function.Name + "]\n")
			}
			if tc.Function.Arguments != "" {
				a.args.WriteString(tc.Function.Arguments)
				// stream the raw JSON arguments so the thinking pane stays live
				sender.Think(tc.Function.Arguments)
				if wd != nil {
					wd.addTokens(len(tc.Function.Arguments)/4 + 1)
				}
			}
		}
	}

	sender.ThinkDone()

	msg := openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleAssistant,
		Content: content.String(),
	}
	for i := 0; i < len(accum); i++ {
		a, ok := accum[i]
		if !ok {
			break
		}
		msg.ToolCalls = append(msg.ToolCalls, openai.ToolCall{
			ID:   a.id,
			Type: openai.ToolTypeFunction,
			Function: openai.FunctionCall{
				Name:      a.name,
				Arguments: a.args.String(),
			},
		})
	}

	return msg, nil
}

// ── tool dispatch ─────────────────────────────────────────────────────────────

func callTool(ctx context.Context, mc *mcpclient.Client, tc openai.ToolCall) (string, map[string]interface{}) {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		msg := fmt.Sprintf("error parsing args: %v", err)
		return msg, nil
	}

	argParts := make([]string, 0, len(args))
	for k, v := range args {
		argParts = append(argParts, fmt.Sprintf("%s=%v", k, v))
	}
	log.Printf("tool call: %s(%s)", tc.Function.Name, strings.Join(argParts, ", "))

	res, err := mc.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      tc.Function.Name,
			Arguments: args,
		},
	})
	if err != nil {
		msg := fmt.Sprintf("tool error: %v", err)
		return msg, nil
	}

	var text string
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			text += tc.Text
		}
	}

	var parsed map[string]interface{}
	json.Unmarshal([]byte(text), &parsed) //nolint:errcheck

	printToolResult(tc.Function.Name, parsed, text)
	return text, parsed
}

func printToolResult(toolName string, parsed map[string]interface{}, raw string) {
	if parsed == nil {
		log.Printf("tool result: %s", raw[:min(len(raw), 120)])
		return
	}
	switch toolName {
	case "submit_guess":
		correct, _ := parsed["correct"].(bool)
		oneAway, _ := parsed["one_away"].(bool)
		status, _ := parsed["status"].(string)
		mistakes, _ := parsed["mistakes_left"].(float64)
		note := "wrong"
		if correct {
			cat, _ := parsed["category"].(map[string]interface{})
			if cat != nil {
				note = fmt.Sprintf("correct category=%v", cat["title"])
			} else {
				note = "correct"
			}
		} else if oneAway {
			note = "one_away"
		}
		log.Printf("tool result: submit_guess %s mistakes_left=%.0f status=%s", note, mistakes, status)
	case "get_state", "get_board":
		remaining, _ := parsed["remaining"].([]interface{})
		ml, _ := parsed["mistakes_left"].(float64)
		status, _ := parsed["status"].(string)
		tried, _ := parsed["tried_combinations"].([]interface{})
		log.Printf("tool result: %s words_left=%d mistakes_left=%.0f tried=%d status=%s",
			toolName, len(remaining), ml, len(tried), status)
	case "suggest_groups":
		groups, _ := parsed["suggested_groups"].([]interface{})
		quality, _ := parsed["overall_quality"].(float64)
		log.Printf("tool result: suggest_groups groups=%d overall_quality=%.2f", len(groups), quality)
	case "get_tried_combinations":
		tried, _ := parsed["tried_combinations"].([]interface{})
		log.Printf("tool result: get_tried_combinations count=%d", len(tried))
	case "memory_note":
		key, _ := parsed["saved"].(string)
		log.Printf("tool result: memory_note saved key=%s", key)
	case "memory_list":
		count, _ := parsed["count"].(float64)
		log.Printf("tool result: memory_list count=%.0f", count)
	case "memory_delete":
		key, _ := parsed["key"].(string)
		deleted, _ := parsed["deleted"].(bool)
		log.Printf("tool result: memory_delete key=%s found=%v", key, deleted)
	case "memory_clear":
		cleared, _ := parsed["cleared"].(float64)
		log.Printf("tool result: memory_clear cleared=%.0f", cleared)
	default:
		log.Printf("tool result: %s %s", toolName, raw[:min(len(raw), 120)])
	}
}

func printGameSummary(r gameResult, s *sessionStats) {
	log.Printf("game %d result: status=%s mistakes=%d/%d turns=%d session=%s",
		r.GameNum, r.Status, r.Mistakes, r.MaxMistakes, r.Turns, r.SessionID)
	if s.Games > 0 {
		log.Printf("session stats: games=%d wins=%d losses=%d win_pct=%d avg_mistakes=%.2f",
			s.Games, s.Wins, s.Losses, 100*s.Wins/s.Games, float64(s.TotalMistakes)/float64(s.Games))
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func convertTools(mcpTools []mcp.Tool) []openai.Tool {
	out := make([]openai.Tool, len(mcpTools))
	for i, t := range mcpTools {
		schema := map[string]interface{}{}
		if raw, err := json.Marshal(t.InputSchema); err == nil {
			json.Unmarshal(raw, &schema) //nolint:errcheck
		}
		out[i] = openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  schema,
			},
		}
	}
	return out
}

func resolveModel(ctx context.Context, ai *openai.Client, model string) (string, error) {
	if model != "" {
		return model, nil
	}
	list, err := ai.ListModels(ctx)
	if err != nil {
		return "", fmt.Errorf("could not list models: %w", err)
	}
	if len(list.Models) == 0 {
		return "", fmt.Errorf("no models loaded — start one in LM Studio first")
	}
	for _, m := range list.Models {
		id := strings.ToLower(m.ID)
		if strings.Contains(id, "embed") || strings.Contains(id, "rerank") {
			continue
		}
		return m.ID, nil
	}
	return list.Models[0].ID, nil
}
