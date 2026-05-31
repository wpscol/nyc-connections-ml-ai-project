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
	return Config{
		Port:        port,
		DBPath:      dbPath,
		MaxMistakes: maxMistakes,
		DataURL:     dataURL,
		MCPBasePath: "/mcp",
	}
}
