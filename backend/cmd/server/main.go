package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

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
	mcpHandler, memStore := mcp.Build(srv, hub, baseURL, embedClient)

	r := chi.NewRouter()
	r.Mount("/", srv.Router())
	r.Mount("/mcp", mcpHandler)
	r.Get("/api/memories", memoriesHandler(memStore))

	log.Printf("server listening on :%s", cfg.Port)
	log.Printf("MCP SSE endpoint: %s/mcp/sse", baseURL)
	if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
		log.Fatal(err)
	}
}

func memoriesHandler(mem *mcp.MemoryStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
		if perPage < 1 {
			perPage = 10
		}

		notes, total := mem.Page(page, perPage)
		pages := (total + perPage - 1) / perPage
		if pages < 1 {
			pages = 1
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"notes":    notes,
			"total":    total,
			"page":     page,
			"per_page": perPage,
			"pages":    pages,
		})
	}
}
