package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

type toolCallDelta struct {
	id   string
	name string
	args strings.Builder
}

// complete calls the model and returns the assistant message, the model's
// thinking for this turn (reasoning_content + content, used to feed the
// <context> buffer), and any error. When sender != nil it streams, routing
// content tokens to the TUI right pane and assembling tool calls from deltas.
// wd monitors the call and cancels it on token/time overrun; pass nil to skip.
//
// reasoning_content is captured for the thinking buffer but stripped from the
// returned message — chain-of-thought must never be fed back as chat history.
func complete(
	ctx context.Context,
	ai *openai.Client,
	req openai.ChatCompletionRequest,
	sender *tuiSender,
	wd *watchdog,
) (openai.ChatCompletionMessage, string, error) {
	// use watchdog context if available so it can interrupt the call
	callCtx := ctx
	if wd != nil {
		callCtx = wd.ctx
	}

	if sender == nil {
		resp, err := ai.CreateChatCompletion(callCtx, req)
		if err != nil {
			return openai.ChatCompletionMessage{}, "", err
		}
		src := resp.Choices[0].Message
		thinking := joinThinking(src.ReasoningContent, src.Content)
		msg := src
		msg.ReasoningContent = "" // never replay CoT into history
		return msg, thinking, nil
	}

	// streaming path
	stream, err := ai.CreateChatCompletionStream(callCtx, req)
	if err != nil {
		return openai.ChatCompletionMessage{}, "", err
	}
	defer stream.Close()

	var content, reasoning strings.Builder
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
			return openai.ChatCompletionMessage{}, "", fmt.Errorf("stream: %w", err)
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta

		// reasoning_content: separate channel many local models (Gemma, QwQ, R1)
		// use for chain-of-thought. Stream it to the pane and capture it for the
		// <context> buffer, but DON'T add it to the assistant message.
		if delta.ReasoningContent != "" {
			sender.Think(delta.ReasoningContent)
			reasoning.WriteString(delta.ReasoningContent)
			if wd != nil {
				wd.addTokens(len(delta.ReasoningContent)/4 + 1)
				if tripRepeat(delta.ReasoningContent) {
					wd.setFired("repetition loop detected (degenerate output)")
					wd.cancel()
				}
			}
		}

		if delta.Content != "" {
			sender.Think(delta.Content)
			content.WriteString(delta.Content)
			// approximate tokens: 1 per 4 chars; watchdog uses this to enforce budget
			if wd != nil {
				wd.addTokens(len(delta.Content)/4 + 1)
				if tripRepeat(delta.Content) {
					wd.setFired("repetition loop detected (degenerate output)")
					wd.cancel()
				}
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

	return msg, joinThinking(reasoning.String(), content.String()), nil
}

// joinThinking combines reasoning_content and visible content into one block for
// the thinking buffer, trimming empties.
func joinThinking(reasoning, content string) string {
	parts := make([]string, 0, 2)
	if r := strings.TrimSpace(reasoning); r != "" {
		parts = append(parts, r)
	}
	if c := strings.TrimSpace(content); c != "" {
		parts = append(parts, c)
	}
	return strings.Join(parts, "\n\n")
}
