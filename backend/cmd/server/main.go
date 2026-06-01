package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"connections/internal/api"
	"connections/internal/config"
	appdb "connections/internal/db"
	"connections/internal/embeddings"
	"connections/internal/fetcher"
	"connections/internal/mcp"
)

func main() {
	cfg := config.Load()

	db, err := appdb.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := fetcher.SeedIfEmpty(db, cfg.DataURL, cfg.MaxMistakes); err != nil {
		log.Printf("warning: seed failed: %v", err)
	}

	hub := api.NewHub()
	srv := api.NewServer(db, hub)

	embedClient := embeddings.NewClient(cfg.EmbedURL, cfg.EmbedModel, cfg.EmbedKey)
	log.Printf("embedding service: %s  model: %s", cfg.EmbedURL, cfg.EmbedModel)

	baseURL := fmt.Sprintf("http://localhost:%s", cfg.Port)
	mcpHandler := mcp.Build(srv, hub, baseURL, embedClient)

	r := chi.NewRouter()
	r.Mount("/", srv.Router())
	r.Mount("/mcp", mcpHandler)

	log.Printf("server listening on :%s", cfg.Port)
	log.Printf("MCP SSE endpoint: %s/mcp/sse", baseURL)
	if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
		log.Fatal(err)
	}
}
