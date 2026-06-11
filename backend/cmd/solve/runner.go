package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	openai "github.com/sashabaranov/go-openai"
)

// runGame plays one game to completion (a win returns; a loss is non-terminal
// and loops via restart_game until solved or a limit aborts).
//
// Prompt injection is purely STAGE-DRIVEN: each relevant game event sets a
// pending stage, and exactly one stage prompt — prefixed with the <context>
// reference block — is injected per turn. No periodic reminders, no progress
// snapshots, no other context is injected anywhere.
func runGame(
	ctx context.Context,
	ai *openai.Client,
	mc *mcpclient.Client,
	model string,
	p prompts,
	tools []openai.Tool,
	maxTokens int,
	wdTimeout time.Duration,
	wdTokenLimit int,
	sender *tuiSender,
	firstGame bool,
) (gameResult, error) {
	result := gameResult{At: time.Now()}
	noToolStreak := 0
	overthinksInRow := 0

	gctx := &gameCtx{}

	// Only the very first game of the process is a true fresh start. Every later
	// game re-enters with memory and a live session already in place, so use the
	// "continue" bootstrap — otherwise the model treats a mid-run resume as a
	// brand-new game and discards its accumulated context.
	pendingStage := StageSessionStart
	if !firstGame {
		pendingStage = StageSessionContinue
	}
	pendingPri := priorityOf(pendingStage)

	setStage := func(s Stage) {
		if priorityOf(s) >= pendingPri {
			pendingStage = s
			pendingPri = priorityOf(s)
		}
	}

	// Tell the model 3/4 of the real watchdog limit as its reasoning budget — the
	// watchdog cancels a call once streamed reasoning+content exceeds wdTokenLimit
	// (approx chars/4), so advertising a lower target leaves a margin to wrap up
	// and act before the hard cutoff hits.
	softBudget := wdTokenLimit * 3 / 4
	systemMsg := strings.ReplaceAll(p.system, "{token_budget}", strconv.Itoa(softBudget))
	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: systemMsg},
	}

	inject := func(role, content string) {
		messages = append(messages, openai.ChatCompletionMessage{Role: role, Content: content})
	}

	// injectStage emits one user message: the <context> block followed by the
	// stage's prompt. This is the sole context-injection point in the loop.
	injectStage := func(s Stage) {
		inject(openai.ChatMessageRoleUser, gctx.contextBlock()+"\n\n"+p.text(s))
		log.Printf("stage injected: %s (pri=%d)", s, priorityOf(s))
	}

	for turn := 0; turn < maxTurns; turn++ {
		gctx.turn = turn + 1

		if pendingStage != StageNone {
			injectStage(pendingStage)
			pendingStage = StageNone
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

		wd := newWatchdog(ctx, wdTimeout, wdTokenLimit)
		msg, thinking, err := complete(ctx, ai, req, sender, wd)
		fired, wdReason := wd.stop()

		if fired || (err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))) {
			// watchdog interrupted the model — it was overthinking
			overthinksInRow++
			log.Printf("watchdog fired on turn %d: %s (strike %d/%d)", turn+1, wdReason, overthinksInRow, maxOverthinks)
			if overthinksInRow >= maxOverthinks {
				return result, fmt.Errorf("model stuck in overthinking loop (%d consecutive watchdog triggers)", overthinksInRow)
			}
			gctx.addThinking(thinking)
			nudge := strings.NewReplacer(
				"{reason}", wdReason,
				"{context}", gctx.factsSummary(),
			).Replace(p.watchdog)
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

		gctx.addThinking(thinking)
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
			inject(openai.ChatMessageRoleUser, "Stop reasoning. Call a tool now. Start with list_sessions.")
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
						for _, w := range strings.Split(ws, ",") {
							submittedWords = append(submittedWords, strings.TrimSpace(strings.ToUpper(w)))
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
			case "get_state", "get_board":
				// reading the board (incl. tried_combinations) precedes a guess
				setStage(StageAnalysis)

			case "restart_game":
				gctx.reset() // fresh board; model replays solved_N from memory
				setStage(StageRestart)

			case "next_game":
				gctx = &gameCtx{} // new puzzle: wipe everything, including thinking

			case "submit_guess":
				correct, _ := parsed["correct"].(bool)
				oneAway, _ := parsed["one_away"].(bool)
				switch {
				case result.Status == "won":
					// handled by the win-return below
				case result.Status == "lost":
					setStage(StageLost)
				case correct:
					setStage(StageCorrect)
				case oneAway:
					setStage(StageOneAway)
				default:
					setStage(StageWrong)
				}
			}

			// A win ends the game — inject game_won.md immediately, then return so
			// the outer loop advances to the next puzzle. A loss does NOT end the
			// game: keep looping so game_lost.md reaches the model and it can save
			// memory and call restart_game (replaying the same puzzle in-place
			// until solved or the turn/watchdog limits hit).
			if result.Status == "won" {
				injectStage(StageWon)
				return result, nil
			}
		}
	}

	if result.Status == "" {
		result.Status = "error"
	}
	return result, nil
}
