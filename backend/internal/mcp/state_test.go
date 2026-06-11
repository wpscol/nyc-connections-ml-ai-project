package mcp

import (
	"strings"
	"testing"

	"connections/internal/game"
)

func wonState() *game.GameState {
	return &game.GameState{
		Status:       "won",
		MistakesLeft: 2,
		MaxMistakes:  4,
		Solved: []game.SolvedGroup{
			{Title: "UNITS OF LENGTH"}, {Title: "LETTER HOMOPHONES"},
			{Title: "FOOTWEAR"}, {Title: "MAGAZINES"},
		},
	}
}

func TestStateTipWonJustInforms(t *testing.T) {
	tip := stateTip(wonState(), 9)
	if !strings.Contains(tip, "WON") || !strings.Contains(tip, "next_game") {
		t.Errorf("won tip should just inform + point at next_game: %s", tip)
	}
	// "just inform it is won" — must NOT leak board/solved detail.
	for _, leak := range []string{"FOOTWEAR", "MAGAZINES", "remain"} {
		if strings.Contains(tip, leak) {
			t.Errorf("won tip should not include %q: %s", leak, tip)
		}
	}
}

func TestStateTipLostAndPlaying(t *testing.T) {
	lost := stateTip(&game.GameState{Status: "lost"}, 5)
	if !strings.Contains(lost, "restart_game") {
		t.Errorf("lost tip should mention restart_game: %s", lost)
	}
	playing := stateTip(&game.GameState{
		Status: "playing", MistakesLeft: 3,
		RemainingWords: []string{"A", "B", "C", "D", "E", "F", "G", "H"},
	}, 4)
	if !strings.Contains(playing, "8 words remain in 2 unsolved group(s)") {
		t.Errorf("playing tip wrong: %s", playing)
	}
}
