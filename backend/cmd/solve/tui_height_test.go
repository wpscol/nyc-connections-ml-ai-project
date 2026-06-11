package main

import (
	"strings"
	"testing"
)

// fillModel returns a TUI model packed with enough wrapped log + reasoning text
// to overflow any pane, at the given terminal size.
func fillModel(w, h int) tuiModel {
	m := newTUI(nil, "model: google/gemma-4-12b-qat  game: 1  W:0 L:1  last: lost (3/4 mistakes)  turn: 13")
	m.w, m.h = w, h
	long := "tool call: submit_guess(session_id=4dbe2dd5-a2e2-44ea-93d1-917ac0470d6b, words=BUS,CAR,TRAIN,METRO)"
	for i := 0; i < 60; i++ {
		m.logLines = append(m.logLines, long)
	}
	m.thinkBuf = strings.Repeat("reasoning about the puzzle words and the hidden groups ", 40)
	return m
}

// The TUI runs in alt-screen: View must never emit more rows than the terminal
// height, or the frame scrolls and the bottom border is pushed off-screen.
func TestViewNeverExceedsHeight(t *testing.T) {
	for h := 3; h <= 50; h++ {
		for _, w := range []int{40, 80, 120} {
			out := fillModel(w, h).View()
			if got := strings.Count(out, "\n") + 1; got > h {
				t.Errorf("w=%d h=%d: View produced %d lines (> %d)", w, h, got, h)
			}
		}
	}
}

// Regression: the per-line wrap width must match each pane's real text area
// (Width minus padding). When it didn't, lipgloss re-wrapped lines into extra
// rows, growing the pane until MaxHeight clipped its bottom border. Assert the
// row directly above the status bar still carries both panes' bottom borders.
func TestPaneBottomBordersPresent(t *testing.T) {
	for h := 6; h <= 50; h++ {
		for _, w := range []int{40, 80, 120} {
			lines := strings.Split(fillModel(w, h).View(), "\n")
			borderRow := lines[len(lines)-2] // status bar is the last row
			if strings.Count(borderRow, "╰") != 2 || strings.Count(borderRow, "╯") != 2 {
				t.Errorf("w=%d h=%d: bottom border row missing both panes: %q", w, h, borderRow)
			}
		}
	}
}
