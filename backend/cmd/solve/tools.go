package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	openai "github.com/sashabaranov/go-openai"
)

// callTool dispatches a model tool call to the MCP server and returns the raw
// text result plus the parsed JSON (nil if it did not parse).
func callTool(ctx context.Context, mc *mcpclient.Client, tc openai.ToolCall) (string, map[string]interface{}) {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		return fmt.Sprintf("error parsing args: %v", err), nil
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
		return fmt.Sprintf("tool error: %v", err), nil
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
