package game

import (
	"testing"
)

var testPuzzle = Puzzle{
	ID:   1,
	Date: "Test Puzzle",
	Categories: []Category{
		{Title: "YELLOW GROUP", Difficulty: 0, Cards: []Card{
			{Content: "A"}, {Content: "B"}, {Content: "C"}, {Content: "D"},
		}},
		{Title: "GREEN GROUP", Difficulty: 1, Cards: []Card{
			{Content: "E"}, {Content: "F"}, {Content: "G"}, {Content: "H"},
		}},
		{Title: "BLUE GROUP", Difficulty: 2, Cards: []Card{
			{Content: "I"}, {Content: "J"}, {Content: "K"}, {Content: "L"},
		}},
		{Title: "PURPLE GROUP", Difficulty: 3, Cards: []Card{
			{Content: "M"}, {Content: "N"}, {Content: "O"}, {Content: "P"},
		}},
	},
}

func TestValidGuess_Correct(t *testing.T) {
	words := []string{"A", "B", "C", "D"}
	correct, cat, oneAway := ValidateGuess(words, testPuzzle, nil)
	if !correct {
		t.Fatal("expected correct guess")
	}
	if cat == nil || cat.Title != "YELLOW GROUP" {
		t.Fatalf("expected YELLOW GROUP, got %v", cat)
	}
	if oneAway {
		t.Error("oneAway should be false on correct guess")
	}
}

func TestValidGuess_Wrong(t *testing.T) {
	words := []string{"A", "B", "C", "E"} // 3 from yellow + 1 from green
	correct, cat, _ := ValidateGuess(words, testPuzzle, nil)
	if correct {
		t.Fatal("expected wrong guess")
	}
	if cat != nil {
		t.Error("category should be nil on wrong guess")
	}
}

func TestValidGuess_OneAway(t *testing.T) {
	words := []string{"A", "B", "C", "E"} // 3 yellow + 1 green
	_, _, oneAway := ValidateGuess(words, testPuzzle, nil)
	if !oneAway {
		t.Fatal("expected oneAway to be true")
	}
}

func TestValidGuess_SkipsSolved(t *testing.T) {
	// Yellow is already solved; guess all yellow words should not match
	words := []string{"A", "B", "C", "D"}
	correct, _, _ := ValidateGuess(words, testPuzzle, []int{0}) // 0=yellow solved
	if correct {
		t.Fatal("should not match already-solved category")
	}
}

func TestAllWords_Count(t *testing.T) {
	words := AllWords(testPuzzle)
	if len(words) != 16 {
		t.Fatalf("expected 16 words, got %d", len(words))
	}
}

func TestRemoveWords(t *testing.T) {
	words := []string{"A", "B", "C", "D", "E"}
	result := RemoveWords(words, []string{"B", "D"})
	if len(result) != 3 {
		t.Fatalf("expected 3 words, got %d", len(result))
	}
	for _, w := range result {
		if w == "B" || w == "D" {
			t.Errorf("word %q should have been removed", w)
		}
	}
}

func TestApplyGuess_Correct(t *testing.T) {
	state := &GameState{
		MistakesLeft:   4,
		MaxMistakes:    4,
		Solved:         []SolvedGroup{},
		RemainingWords: AllWords(testPuzzle),
		Status:         "playing",
	}
	cat := &testPuzzle.Categories[0]
	result := &GuessResult{Correct: true}
	ApplyGuess(state, result, cat)

	if len(state.Solved) != 1 {
		t.Fatalf("expected 1 solved group, got %d", len(state.Solved))
	}
	if state.MistakesLeft != 4 {
		t.Error("mistakes should not change on correct guess")
	}
	if len(state.RemainingWords) != 12 {
		t.Fatalf("expected 12 remaining, got %d", len(state.RemainingWords))
	}
}

func TestApplyGuess_Wrong_DecrementsMistakes(t *testing.T) {
	state := &GameState{MistakesLeft: 2, Status: "playing", Solved: []SolvedGroup{}}
	result := &GuessResult{Correct: false}
	ApplyGuess(state, result, nil)
	if state.MistakesLeft != 1 {
		t.Fatalf("expected 1 mistake left, got %d", state.MistakesLeft)
	}
	if state.Status != "playing" {
		t.Errorf("status should still be playing")
	}
}

func TestApplyGuess_LastMistake_SetsLost(t *testing.T) {
	state := &GameState{MistakesLeft: 1, Status: "playing", Solved: []SolvedGroup{}}
	result := &GuessResult{Correct: false}
	ApplyGuess(state, result, nil)
	if state.Status != "lost" {
		t.Fatalf("expected status=lost, got %q", state.Status)
	}
}

func TestApplyGuess_AllSolved_SetsWon(t *testing.T) {
	state := &GameState{
		MistakesLeft:   4,
		Solved:         make([]SolvedGroup, 3), // 3 already solved
		RemainingWords: []string{"M", "N", "O", "P"},
		Status:         "playing",
	}
	cat := &testPuzzle.Categories[3]
	result := &GuessResult{Correct: true}
	ApplyGuess(state, result, cat)
	if state.Status != "won" {
		t.Fatalf("expected status=won, got %q", state.Status)
	}
}

func TestComputeStats_PerfectWin(t *testing.T) {
	state := &GameState{
		MaxMistakes:  4,
		MistakesLeft: 4,
		Status:       "won",
		Solved:       make([]SolvedGroup, 4),
		Guesses: []GuessAttempt{
			{Correct: true}, {Correct: true}, {Correct: true}, {Correct: true},
		},
	}
	s := ComputeStats(state)
	if s.TotalGuesses != 4 || s.CorrectGuesses != 4 {
		t.Fatalf("expected 4/4 guesses, got %d/%d", s.CorrectGuesses, s.TotalGuesses)
	}
	if s.GroupsSolved != 4 {
		t.Errorf("expected 4 groups solved, got %d", s.GroupsSolved)
	}
	if s.Mistakes != 0 {
		t.Errorf("expected 0 mistakes, got %d", s.Mistakes)
	}
	if s.Accuracy != 100 {
		t.Errorf("expected 100%% accuracy, got %d", s.Accuracy)
	}
	if !s.Won {
		t.Error("expected Won=true")
	}
}

func TestComputeStats_MixedAndLost(t *testing.T) {
	state := &GameState{
		MaxMistakes:  4,
		MistakesLeft: 0,
		Status:       "lost",
		Solved:       make([]SolvedGroup, 2),
		Guesses: []GuessAttempt{
			{Correct: true}, {Correct: false}, {Correct: true},
			{Correct: false}, {Correct: false}, {Correct: false},
		},
	}
	s := ComputeStats(state)
	if s.TotalGuesses != 6 || s.CorrectGuesses != 2 {
		t.Fatalf("expected 2/6 guesses, got %d/%d", s.CorrectGuesses, s.TotalGuesses)
	}
	if s.Mistakes != 4 {
		t.Errorf("expected 4 mistakes, got %d", s.Mistakes)
	}
	if s.Accuracy != 33 { // 2/6 = 33%
		t.Errorf("expected 33%% accuracy, got %d", s.Accuracy)
	}
	if s.Won {
		t.Error("expected Won=false")
	}
}

func TestComputeStats_NoGuesses(t *testing.T) {
	state := &GameState{MaxMistakes: 4, MistakesLeft: 4, Status: "playing"}
	s := ComputeStats(state)
	if s.TotalGuesses != 0 || s.Accuracy != 0 {
		t.Fatalf("expected zero guesses and 0%% accuracy, got %d guesses %d%%", s.TotalGuesses, s.Accuracy)
	}
}
