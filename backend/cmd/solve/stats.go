package main

import (
	"log"
	"time"
)

// gameResult captures the outcome of a single game run.
type gameResult struct {
	GameNum     int
	Status      string
	Mistakes    int
	MaxMistakes int
	Turns       int
	SessionID   string
	At          time.Time
}

// sessionStats aggregates results across all games in a run.
type sessionStats struct {
	Games         int
	Wins          int
	Losses        int
	TotalMistakes int
}

func (s *sessionStats) record(r gameResult) {
	s.Games++
	if r.Status == "won" {
		s.Wins++
	} else {
		s.Losses++
	}
	s.TotalMistakes += r.Mistakes
}

func (s *sessionStats) print() {
	if s.Games == 0 {
		return
	}
	log.Printf("final stats: games=%d wins=%d losses=%d win_pct=%d total_mistakes=%d avg_mistakes=%.2f",
		s.Games, s.Wins, s.Losses, 100*s.Wins/s.Games, s.TotalMistakes,
		float64(s.TotalMistakes)/float64(s.Games))
}

func printGameSummary(r gameResult, s *sessionStats) {
	log.Printf("game %d result: status=%s mistakes=%d/%d turns=%d session=%s",
		r.GameNum, r.Status, r.Mistakes, r.MaxMistakes, r.Turns, r.SessionID)
	if s.Games > 0 {
		log.Printf("session stats: games=%d wins=%d losses=%d win_pct=%d avg_mistakes=%.2f",
			s.Games, s.Wins, s.Losses, 100*s.Wins/s.Games, float64(s.TotalMistakes)/float64(s.Games))
	}
}
