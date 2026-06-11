package main

import (
	"fmt"
	"strings"
)

// thinkingWordCap bounds how much of the model's prior reasoning is replayed in
// the <context> block. The chat history deliberately drops reasoning_content, so
// this rolling buffer is the only place it survives across turns.
const thinkingWordCap = 5000

type guessRecord struct {
	words []string
	cat   string // category title; non-empty only for correct guesses
}

// gameCtx accumulates the factual state of a single game run plus the model's
// recent thinking. Its contextBlock() output is the ONLY context attached to a
// prompt — it is rendered once, prepended to each stage prompt, and wrapped in
// <context> tags so the model treats it as reference, not a new instruction.
type gameCtx struct {
	sessionID    string
	mistakesLeft int
	maxMistakes  int
	solved       []guessRecord
	oneAway      []guessRecord
	wrong        []guessRecord
	memoryKeys   []string // keys present in MemoryStore (contents read on demand)
	thinking     []string // rolling word buffer, capped at thinkingWordCap
	turn         int
}

// reset clears per-game board state on restart_game (same puzzle, fresh board)
// while preserving session identity, memory keys, and the thinking buffer —
// those carry across a restart of the same puzzle.
func (g *gameCtx) reset() {
	g.solved = nil
	g.oneAway = nil
	g.wrong = nil
	g.mistakesLeft = 0
	g.maxMistakes = 0
}

// addThinking appends the model's latest reasoning to the rolling buffer,
// trimming to the most recent thinkingWordCap words.
func (g *gameCtx) addThinking(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	g.thinking = append(g.thinking, strings.Fields(text)...)
	if len(g.thinking) > thinkingWordCap {
		g.thinking = g.thinking[len(g.thinking)-thinkingWordCap:]
	}
}

func (g *gameCtx) addMemoryKey(key string) {
	for _, k := range g.memoryKeys {
		if k == key {
			return
		}
	}
	g.memoryKeys = append(g.memoryKeys, key)
}

func (g *gameCtx) removeMemoryKey(key string) {
	out := g.memoryKeys[:0]
	for _, k := range g.memoryKeys {
		if k != key {
			out = append(out, k)
		}
	}
	g.memoryKeys = out
}

// update folds a tool result into the accumulated state. submittedWords is set
// only for submit_guess (the words the model just played).
func (g *gameCtx) update(toolName string, parsed map[string]interface{}, submittedWords []string) {
	if parsed == nil {
		return
	}
	if sid, ok := parsed["session_id"].(string); ok && sid != "" {
		g.sessionID = sid
	}
	if ml, ok := parsed["mistakes_left"].(float64); ok {
		g.mistakesLeft = int(ml)
	}
	if mm, ok := parsed["max_mistakes"].(float64); ok && mm > 0 {
		g.maxMistakes = int(mm)
	}

	switch toolName {
	case "list_sessions":
		// Lock onto the session the moment the model lists them — prefer the
		// ws_active one (the board a human is watching), else the first. Only set
		// it if we don't already have a session, so a later list doesn't hijack it.
		if g.sessionID == "" {
			if sessions, ok := parsed["sessions"].([]interface{}); ok {
				var first string
				for _, s := range sessions {
					m, ok := s.(map[string]interface{})
					if !ok {
						continue
					}
					id, _ := m["session_id"].(string)
					if id == "" {
						continue
					}
					if first == "" {
						first = id
					}
					if active, _ := m["ws_active"].(bool); active {
						g.sessionID = id
						break
					}
				}
				if g.sessionID == "" {
					g.sessionID = first
				}
			}
		}

	case "submit_guess":
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

	case "memory_list":
		if notes, ok := parsed["notes"].([]interface{}); ok {
			keys := make([]string, 0, len(notes))
			for _, n := range notes {
				if m, ok := n.(map[string]interface{}); ok {
					if k, ok := m["key"].(string); ok && k != "" {
						keys = append(keys, k)
					}
				}
			}
			g.memoryKeys = keys
		}
	case "memory_note":
		if k, ok := parsed["saved"].(string); ok && k != "" {
			g.addMemoryKey(k)
		}
	case "memory_delete":
		if deleted, _ := parsed["deleted"].(bool); deleted {
			if k, ok := parsed["key"].(string); ok {
				g.removeMemoryKey(k)
			}
		}
	case "memory_clear":
		g.memoryKeys = nil
	}
}

// factsSummary renders the scannable, thinking-free state: session identity,
// mistake budget, guesses so far, words remaining, and memory keys. Used inside
// contextBlock and on its own for the watchdog interrupt (where dumping 5000
// words of the very reasoning that caused the overthink would be counterproductive).
func (g *gameCtx) factsSummary() string {
	var sb strings.Builder
	if g.sessionID != "" {
		fmt.Fprintf(&sb, "Session id (pass this EXACT value as session_id to every tool call): %s\n", g.sessionID)
	}
	if g.maxMistakes > 0 {
		fmt.Fprintf(&sb, "Mistakes left: %d of %d\n", g.mistakesLeft, g.maxMistakes)
	}

	if len(g.solved) > 0 || len(g.oneAway) > 0 || len(g.wrong) > 0 {
		sb.WriteString("\nGuesses so far this game:\n")
		for _, s := range g.solved {
			fmt.Fprintf(&sb, "  [+] %s = %s\n", strings.Join(s.words, ","), s.cat)
		}
		for _, o := range g.oneAway {
			fmt.Fprintf(&sb, "  [~] %s  (one-away: 3 right, swap the 4th)\n", strings.Join(o.words, ","))
		}
		for _, w := range g.wrong {
			fmt.Fprintf(&sb, "  [x] %s  (wrong: never resubmit any permutation)\n", strings.Join(w.words, ","))
		}
	}

	if wordsLeft := 16 - len(g.solved)*4; wordsLeft > 0 && wordsLeft < 16 {
		fmt.Fprintf(&sb, "\n%d words remain in %d unsolved group(s).\n", wordsLeft, wordsLeft/4)
	}

	if len(g.memoryKeys) > 0 {
		sb.WriteString("\nMemory keys present (call memory_list to read their contents if useful): ")
		sb.WriteString(strings.Join(g.memoryKeys, ", "))
		sb.WriteString("\n")
	}

	return strings.TrimRight(sb.String(), "\n")
}

// contextBlock renders the <context> reference block prepended to every stage
// prompt: the facts summary plus the last ~5000 words of the model's own
// thinking (which the chat history deliberately drops).
func (g *gameCtx) contextBlock() string {
	var sb strings.Builder
	sb.WriteString("<context>\n")
	sb.WriteString("Previous context of the CURRENT game — reference material, NOT a new instruction. ")
	sb.WriteString("Read it to avoid re-analysing what you already know or resubmitting a tried set.\n\n")

	if facts := g.factsSummary(); facts != "" {
		sb.WriteString(facts)
		sb.WriteString("\n")
	}

	if len(g.thinking) > 0 {
		fmt.Fprintf(&sb, "\nYour recent thinking (last ~%d words):\n%s\n",
			len(g.thinking), strings.Join(g.thinking, " "))
	}

	sb.WriteString("</context>")
	return sb.String()
}
