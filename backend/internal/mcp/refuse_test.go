package mcp

import (
	"errors"
	"testing"

	"connections/internal/game"
)

// fakeAPI embeds gameAPI so only GetSession needs a real implementation; the
// other methods are never called by refuseIfNotWon.
type fakeAPI struct {
	gameAPI
	status string
}

func (f fakeAPI) GetSession(id string) (*game.GameState, error) {
	if id == "missing" {
		return nil, errors.New("nope")
	}
	return &game.GameState{Status: f.status}, nil
}

func TestRefuseIfNotWon(t *testing.T) {
	for _, c := range []struct {
		status string
		wantOK bool
	}{
		{"won", true},
		{"lost", false},
		{"playing", false},
		{"", false},
	} {
		if _, ok := refuseIfNotWon(fakeAPI{status: c.status}, "s1", "next_game"); ok != c.wantOK {
			t.Errorf("status=%q: ok=%v want %v", c.status, ok, c.wantOK)
		}
	}
	if msg, ok := refuseIfNotWon(fakeAPI{status: "won"}, "missing", "next_game"); ok || msg == "" {
		t.Errorf("missing session should refuse with message, got ok=%v msg=%q", ok, msg)
	}
}
