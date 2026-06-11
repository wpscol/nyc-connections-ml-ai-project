package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Stage identifies a moment in the game where the solver injects one focused
// prompt. Exactly one stage prompt is injected per relevant event — there is no
// other context injected into the conversation (see runner.go).
type Stage int

const (
	StageNone Stage = iota
	StageSessionStart
	StageSessionContinue
	StageAnalysis
	StageCorrect
	StageOneAway
	StageWrong
	StageWon
	StageLost
	StageRestart
)

// Injection priority when several stages would fire on the same turn — the
// higher value wins. Bootstrap only fires on turn 0, so it sits on top.
const (
	priNone      = 0
	priAnalysis  = 1
	priRestart   = 2
	priResult    = 3
	priBootstrap = 4
)

// stageSpec backs one Stage with a prompt file and an injection priority.
type stageSpec struct {
	file     string
	priority int
}

// stageRegistry is the single source of truth for game stages. To add a stage:
// declare a Stage constant, register it here, and drop its .md file in the
// prompt directory — no other code needs to change.
var stageRegistry = map[Stage]stageSpec{
	StageSessionStart:    {file: "session_start.md", priority: priBootstrap},
	StageSessionContinue: {file: "session_continue.md", priority: priBootstrap},
	StageAnalysis:        {file: "analysis.md", priority: priAnalysis},
	StageCorrect:         {file: "result_correct.md", priority: priResult},
	StageOneAway:         {file: "result_one_away.md", priority: priResult},
	StageWrong:           {file: "result_wrong.md", priority: priResult},
	StageWon:             {file: "game_won.md", priority: priResult},
	StageLost:            {file: "game_lost.md", priority: priResult},
	StageRestart:         {file: "restart.md", priority: priRestart},
}

// prompts holds every prompt the solver injects: the system message, the
// watchdog interrupt, and one text per game stage.
type prompts struct {
	system   string
	watchdog string // overthinking interrupt; placeholders {reason}, {context}
	stages   map[Stage]string
}

func (p prompts) text(s Stage) string { return p.stages[s] }

func priorityOf(s Stage) int { return stageRegistry[s].priority }

func (s Stage) String() string {
	if spec, ok := stageRegistry[s]; ok {
		return strings.TrimSuffix(spec.file, ".md")
	}
	return "none"
}

// loadPrompts reads the system prompt, the watchdog prompt, and every stage
// prompt named in stageRegistry. It fails fast (before any network connection)
// if a file is missing or empty.
func loadPrompts(dir string) (prompts, error) {
	read := func(name string) (string, error) {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return "", fmt.Errorf("%s: %w", name, err)
		}
		text := strings.TrimSpace(string(data))
		if text == "" {
			return "", fmt.Errorf("%s: prompt file is empty", name)
		}
		return text, nil
	}

	var p prompts
	var err error
	if p.system, err = read("system.md"); err != nil {
		return p, err
	}
	if p.watchdog, err = read("watchdog.md"); err != nil {
		return p, err
	}
	p.stages = make(map[Stage]string, len(stageRegistry))
	for stage, spec := range stageRegistry {
		text, err := read(spec.file)
		if err != nil {
			return p, err
		}
		p.stages[stage] = text
	}
	return p, nil
}
