package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port         string
	DBPath       string
	MaxMistakes  int
	DataURL      string
	MCPBasePath  string
	// Embedding service (OpenAI-compatible, e.g. LM Studio)
	EmbedURL   string // base URL, e.g. http://localhost:1234
	EmbedModel string // e.g. text-embedding-nomic-embed-text-v1.5
	EmbedKey   string // API key — optional for local servers
}

func Load() Config {
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
	return Config{
		Port:        port,
		DBPath:      dbPath,
		MaxMistakes: maxMistakes,
		DataURL:     dataURL,
		MCPBasePath: "/mcp",
		EmbedURL:    embedURL,
		EmbedModel:  embedModel,
		EmbedKey:    os.Getenv("EMBED_KEY"),
	}
}
