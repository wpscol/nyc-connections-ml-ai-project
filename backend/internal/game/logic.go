package game

import (
	"math/rand"
)

// ValidateGuess checks if words form a valid unsolved category.
// Returns correct, matched category, and oneAway flag.
func ValidateGuess(words []string, puzzle Puzzle, solvedDifficulties []int) (bool, *Category, bool) {
	solvedSet := make(map[int]bool, len(solvedDifficulties))
	for _, d := range solvedDifficulties {
		solvedSet[d] = true
	}

	oneAway := false
	for i := range puzzle.Categories {
		cat := &puzzle.Categories[i]
		if solvedSet[cat.Difficulty] {
			continue
		}
		matches := countMatches(words, cat)
		if matches == 4 {
			return true, cat, false
		}
		if matches == 3 {
			oneAway = true
		}
	}
	return false, nil, oneAway
}

func countMatches(words []string, cat *Category) int {
	catWords := make(map[string]bool, len(cat.Cards))
	for _, c := range cat.Cards {
		catWords[c.Content] = true
	}
	n := 0
	for _, w := range words {
		if catWords[w] {
			n++
		}
	}
	return n
}

// AllWords returns all words from a puzzle shuffled.
func AllWords(puzzle Puzzle) []string {
	words := make([]string, 0, 16)
	for _, cat := range puzzle.Categories {
		for _, card := range cat.Cards {
			words = append(words, card.Content)
		}
	}
	rand.Shuffle(len(words), func(i, j int) { words[i], words[j] = words[j], words[i] })
	return words
}

// SolvedDifficulties extracts difficulty levels from solved groups.
func SolvedDifficulties(solved []SolvedGroup) []int {
	out := make([]int, len(solved))
	for i, s := range solved {
		out[i] = s.Difficulty
	}
	return out
}

// RemoveWords returns words slice with given words removed.
func RemoveWords(words, toRemove []string) []string {
	remove := make(map[string]bool, len(toRemove))
	for _, w := range toRemove {
		remove[w] = true
	}
	out := words[:0:0]
	for _, w := range words {
		if !remove[w] {
			out = append(out, w)
		}
	}
	return out
}
