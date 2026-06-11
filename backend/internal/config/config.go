package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port        string
	DBPath      string
	MaxMistakes int
	DataURL     string
	MCPBasePath string
	// Embedding service (OpenAI-compatible, e.g. LM Studio)
	EmbedURL   string // base URL, e.g. http://localhost:1234
	EmbedModel string // e.g. text-embedding-nomic-embed-text-v1.5
	EmbedKey   string // API key — optional for local servers
	// Solver
	PromptDir          string
	WatchdogTimeout    time.Duration
	WatchdogTokenLimit int
}

// loadEnvFiles reads KEY=VALUE pairs from each path in order. Later files
// override earlier ones. Blank lines and # comments are ignored. Values are
// only applied when the key is not already set in the process environment,
// so real env vars (CI, shell exports) always win.
func loadEnvFiles(paths ...string) {
	merged := map[string]string{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue // missing file is fine — config.local is optional
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			merged[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	for k, v := range merged {
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}

func Load() Config {
	// config.default is the baseline (checked in). config.local overrides it
	// (git-ignored, safe for local secrets and paths). Real env vars win over both.
	loadEnvFiles(".env.default", ".env.local")

	maxMistakes := 4
	if v := os.Getenv("MAX_MISTAKES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxMistakes = n
		}
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./connections.db"
	}
	dataURL := os.Getenv("DATA_URL")
	if dataURL == "" {
		dataURL = "https://raw.githubusercontent.com/Eyefyre/NYT-Connections-Answers/main/connections.json"
	}
	embedURL := os.Getenv("EMBED_URL")
	if embedURL == "" {
		embedURL = "http://localhost:1234"
	}
	embedModel := os.Getenv("EMBED_MODEL") // empty = auto-discover from LM Studio /v1/models

	promptDir := os.Getenv("PROMPT_DIR")
	if promptDir == "" {
		promptDir = "../PROMPT"
	}

	wdTimeout := 3 * time.Minute
	if v := os.Getenv("WATCHDOG_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			wdTimeout = d
		}
	}

	wdTokenLimit := 2000
	if v := os.Getenv("WATCHDOG_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			wdTokenLimit = n
		}
	}

	return Config{
		Port:               port,
		DBPath:             dbPath,
		MaxMistakes:        maxMistakes,
		DataURL:            dataURL,
		MCPBasePath:        "/mcp",
		EmbedURL:           embedURL,
		EmbedModel:         embedModel,
		EmbedKey:           os.Getenv("EMBED_KEY"),
		PromptDir:          promptDir,
		WatchdogTimeout:    wdTimeout,
		WatchdogTokenLimit: wdTokenLimit,
	}
}
