package main

import (
	"strings"
	"testing"
)

func TestContextBlockHasFactsAndThinking(t *testing.T) {
	g := &gameCtx{}
	g.update("get_state", map[string]interface{}{
		"session_id":    "abcd1234-aaaa-bbbb",
		"mistakes_left": float64(3),
		"max_mistakes":  float64(4),
	}, nil)
	g.update("submit_guess", map[string]interface{}{
		"correct":  true,
		"category": map[string]interface{}{"title": "DOG BREEDS"},
	}, []string{"PUG", "BOXER", "LAB", "CORGI"})
	g.update("submit_guess", map[string]interface{}{"one_away": true}, []string{"A", "B", "C", "D"})
	g.update("submit_guess", map[string]interface{}{}, []string{"W", "X", "Y", "Z"})
	g.addThinking("the purple group is probably a wordplay set")

	block := g.contextBlock()

	for _, want := range []string{
		"<context>", "</context>",
		"abcd1234-aaaa-bbbb",
		"Mistakes left: 3 of 4",
		"PUG,BOXER,LAB,CORGI = DOG BREEDS",
		"one-away",
		"wrong",
		"12 words remain in 3 unsolved group(s)",
		"wordplay set",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("context block missing %q\n---\n%s", want, block)
		}
	}
}

func TestFactsSummaryOmitsThinking(t *testing.T) {
	g := &gameCtx{}
	g.addThinking("secret chain of thought tokens")
	if strings.Contains(g.factsSummary(), "secret chain") {
		t.Error("factsSummary must not include the thinking buffer")
	}
}

func TestSessionIDFromListSessions(t *testing.T) {
	g := &gameCtx{}
	g.update("list_sessions", map[string]interface{}{
		"sessions": []interface{}{
			map[string]interface{}{"session_id": "inactive-one", "ws_active": false},
			map[string]interface{}{"session_id": "watched-two", "ws_active": true},
		},
	}, nil)
	if g.sessionID != "watched-two" {
		t.Errorf("should lock onto ws_active session, got %q", g.sessionID)
	}
	// a later list must not hijack the locked session
	g.update("list_sessions", map[string]interface{}{
		"sessions": []interface{}{map[string]interface{}{"session_id": "other", "ws_active": true}},
	}, nil)
	if g.sessionID != "watched-two" {
		t.Errorf("session id should not change once locked, got %q", g.sessionID)
	}
	if !strings.Contains(g.contextBlock(), "watched-two") {
		t.Error("context block should remind the model of the session id")
	}
}

func TestThinkingCap(t *testing.T) {
	g := &gameCtx{}
	g.addThinking(strings.Repeat("word ", thinkingWordCap+500))
	if len(g.thinking) != thinkingWordCap {
		t.Errorf("thinking not capped: got %d want %d", len(g.thinking), thinkingWordCap)
	}
}

func TestMemoryKeyTracking(t *testing.T) {
	g := &gameCtx{}
	g.update("memory_list", map[string]interface{}{
		"notes": []interface{}{
			map[string]interface{}{"key": "solved_1"},
			map[string]interface{}{"key": "hunch_colors"},
		},
	}, nil)
	g.update("memory_note", map[string]interface{}{"saved": "solved_2"}, nil)
	g.update("memory_note", map[string]interface{}{"saved": "solved_1"}, nil) // dup, no-op
	g.update("memory_delete", map[string]interface{}{"deleted": true, "key": "hunch_colors"}, nil)

	got := strings.Join(g.memoryKeys, ",")
	if got != "solved_1,solved_2" {
		t.Errorf("memory keys = %q, want solved_1,solved_2", got)
	}

	g.update("memory_clear", map[string]interface{}{"cleared": float64(2)}, nil)
	if len(g.memoryKeys) != 0 {
		t.Errorf("memory_clear should wipe keys, got %v", g.memoryKeys)
	}
}

func TestResetKeepsThinkingAndKeys(t *testing.T) {
	g := &gameCtx{}
	g.addThinking("carry me across the restart")
	g.update("memory_note", map[string]interface{}{"saved": "solved_1"}, nil)
	g.update("submit_guess", map[string]interface{}{}, []string{"A", "B", "C", "D"})

	g.reset()

	if len(g.wrong) != 0 {
		t.Error("reset should clear board guesses")
	}
	if len(g.thinking) == 0 {
		t.Error("reset should preserve thinking")
	}
	if len(g.memoryKeys) != 1 {
		t.Error("reset should preserve memory keys")
	}
}

func TestStageRegistryComplete(t *testing.T) {
	// every registered stage must resolve to a non-empty prompt file name and a
	// known priority — guards against half-added stages.
	for stage, spec := range stageRegistry {
		if spec.file == "" {
			t.Errorf("stage %v has empty file", stage)
		}
		if spec.priority < priAnalysis || spec.priority > priBootstrap {
			t.Errorf("stage %v has out-of-range priority %d", stage, spec.priority)
		}
		if stage.String() == "none" {
			t.Errorf("stage %v stringifies to none", stage)
		}
	}
}
