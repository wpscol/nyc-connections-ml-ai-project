// mcpdemo connects to the running MCP SSE server and solves the full puzzle,
// submitting one group every 5 seconds to show real-time WebSocket updates.
package main

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const base = "http://localhost:8080"

type jsonRPC struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

func main() {
	// ── 1. Load all puzzle categories from DB ─────────────────────────────
	db, err := sql.Open("sqlite", "./connections.db")
	if err != nil {
		log.Fatal("open db:", err)
	}
	defer db.Close()

	var idStr string
	db.QueryRow(`SELECT value FROM config WHERE key='current_puzzle_id'`).Scan(&idStr)

	var rawData string
	db.QueryRow(`SELECT data FROM puzzles WHERE id=?`, idStr).Scan(&rawData)

	var puzzle struct {
		Date       string `json:"date"`
		Categories []struct {
			Title      string `json:"title"`
			Difficulty int    `json:"difficulty"`
			Cards      []struct {
				Content string `json:"content"`
			} `json:"cards"`
		} `json:"categories"`
	}
	if err := json.Unmarshal([]byte(rawData), &puzzle); err != nil {
		log.Fatal("unmarshal puzzle:", err)
	}
	if len(puzzle.Categories) == 0 {
		log.Fatal("no categories in puzzle")
	}

	// Sort easiest → hardest (yellow → purple)
	sort.Slice(puzzle.Categories, func(i, j int) bool {
		return puzzle.Categories[i].Difficulty < puzzle.Categories[j].Difficulty
	})

	fmt.Printf("Puzzle %s (%s) — %d groups\n\n", idStr, puzzle.Date, len(puzzle.Categories))

	// ── 2. Find browser's session (active WS) or create one ───────────────
	sessionID := ""
	{
		r, err := http.Get(base + "/api/sessions")
		if err != nil {
			log.Fatal("list sessions:", err)
		}
		var list []struct {
			ID       string `json:"session_id"`
			Status   string `json:"status"`
			WSActive bool   `json:"ws_active"`
		}
		json.NewDecoder(r.Body).Decode(&list)
		r.Body.Close()

		for _, s := range list {
			if s.Status == "playing" && s.WSActive {
				sessionID = s.ID
				break
			}
		}
		if sessionID == "" {
			for _, s := range list {
				if s.Status == "playing" {
					sessionID = s.ID
					break
				}
			}
		}
	}
	if sessionID != "" {
		fmt.Printf("Using browser session: %s\n\n", sessionID)
	} else {
		r, err := http.Post(base+"/api/session", "application/json", strings.NewReader("{}"))
		if err != nil {
			log.Fatal("create session:", err)
		}
		var sess struct {
			SessionID string `json:"session_id"`
		}
		json.NewDecoder(r.Body).Decode(&sess)
		r.Body.Close()
		sessionID = sess.SessionID
		fmt.Printf("Created new session: %s\n\n", sessionID)
	}

	// ── 3. Open MCP SSE connection ────────────────────────────────────────
	sseReq, _ := http.NewRequest("GET", base+"/mcp/sse", nil)
	sseReq.Header.Set("Accept", "text/event-stream")
	sseResp, err := http.DefaultClient.Do(sseReq)
	if err != nil {
		log.Fatal("SSE connect:", err)
	}
	defer sseResp.Body.Close()

	scanner := bufio.NewScanner(sseResp.Body)
	msgEndpoint := ""
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			msgEndpoint = strings.TrimPrefix(line, "data: ")
			break
		}
	}

	ch := make(chan string, 16)
	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				ch <- strings.TrimPrefix(line, "data: ")
			}
		}
	}()

	callMCP := func(id int, method string, params interface{}) map[string]interface{} {
		body, _ := json.Marshal(jsonRPC{"2.0", id, method, params})
		r, err := http.Post(msgEndpoint, "application/json", bytes.NewReader(body))
		if err != nil {
			log.Fatalf("POST %s: %v", method, err)
		}
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
		select {
		case data := <-ch:
			var out map[string]interface{}
			json.Unmarshal([]byte(data), &out)
			return out
		case <-time.After(8 * time.Second):
			log.Fatalf("timeout waiting for %s", method)
			return nil
		}
	}

	// ── 4. Initialize ─────────────────────────────────────────────────────
	callMCP(1, "initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "mcpdemo", "version": "1.0"},
	})
	fmt.Println("MCP initialized\n")

	// ── 5. Submit all 4 groups, one every 5 seconds ───────────────────────
	diffLabel := map[int]string{0: "🟡 yellow", 1: "🟢 green", 2: "🔵 blue", 3: "🟣 purple"}

	for i, cat := range puzzle.Categories {
		words := make([]string, len(cat.Cards))
		for j, c := range cat.Cards {
			words[j] = c.Content
		}

		fmt.Printf("[%d/4] %s — %s\n", i+1, diffLabel[cat.Difficulty], cat.Title)
		fmt.Printf("      submitting: %s\n", strings.Join(words, ", "))

		resp := callMCP(10+i, "tools/call", map[string]interface{}{
			"name": "submit_guess",
			"arguments": map[string]string{
				"session_id": sessionID,
				"words":      strings.Join(words, ","),
			},
		})

		// Parse result
		if result, ok := resp["result"]; ok {
			if content, ok := result.(map[string]interface{})["content"]; ok {
				if arr, ok := content.([]interface{}); ok && len(arr) > 0 {
					if item, ok := arr[0].(map[string]interface{}); ok {
						var gr map[string]interface{}
						json.Unmarshal([]byte(item["text"].(string)), &gr)
						if gr["correct"].(bool) {
							fmt.Printf("      ✓ correct  (mistakes left: %.0f, status: %v)\n", gr["mistakes_left"], gr["status"])
						} else {
							fmt.Printf("      ✗ wrong  (one_away: %v, mistakes left: %.0f)\n", gr["one_away"], gr["mistakes_left"])
						}
					}
				}
			}
		}

		if i < len(puzzle.Categories)-1 {
			fmt.Println("      waiting 5s…")
			time.Sleep(5 * time.Second)
			fmt.Println()
		}
	}

	fmt.Println("\n✓ Puzzle complete — all groups solved via MCP.")
}
