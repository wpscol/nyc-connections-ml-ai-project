package fetcher

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	appdb "connections/internal/db"
	"connections/internal/game"
)

// rawPuzzle matches the actual GitHub JSON schema.
type rawPuzzle struct {
	ID      int64       `json:"id"`
	Date    string      `json:"date"`
	Answers []rawAnswer `json:"answers"`
}

type rawAnswer struct {
	Level   int      `json:"level"`
	Group   string   `json:"group"`
	Members []string `json:"members"`
}

// fetchData reads puzzle JSON from a URL (http/https) or a local file path.
// A "file://" prefix is stripped; anything else without a recognized scheme
// is treated as a plain filesystem path.
func fetchData(source string) ([]byte, error) {
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		resp, err := http.Get(source)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		return io.ReadAll(resp.Body)
	}
	path := strings.TrimPrefix(source, "file://")
	return os.ReadFile(path)
}

// SeedIfEmpty fetches puzzles from dataURL and inserts them if DB is empty.
func SeedIfEmpty(db *sql.DB, dataURL string, defaultMaxMistakes int) error {
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM puzzles`).Scan(&count)
	if count > 0 {
		return nil
	}

	body, err := fetchData(dataURL)
	if err != nil {
		return fmt.Errorf("fetch data: %w", err)
	}

	var rawPuzzles []rawPuzzle
	if err := json.Unmarshal(body, &rawPuzzles); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, rp := range rawPuzzles {
		// Convert raw format → internal game.Puzzle format
		puzzle := game.Puzzle{
			ID:         rp.ID,
			Date:       rp.Date,
			Categories: make([]game.Category, 0, len(rp.Answers)),
		}
		for _, a := range rp.Answers {
			cat := game.Category{
				Title:      a.Group,
				Difficulty: a.Level,
				Cards:      make([]game.Card, 0, len(a.Members)),
			}
			for i, m := range a.Members {
				cat.Cards = append(cat.Cards, game.Card{Content: m, Position: i})
			}
			puzzle.Categories = append(puzzle.Categories, cat)
		}

		data, err := json.Marshal(puzzle)
		if err != nil {
			continue
		}
		_, err = tx.Exec(
			`INSERT OR IGNORE INTO puzzles(id,date,data) VALUES(?,?,?)`,
			puzzle.ID, puzzle.Date, string(data),
		)
		if err != nil {
			return fmt.Errorf("insert puzzle %d: %w", puzzle.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	if len(rawPuzzles) > 0 {
		if err := appdb.SetConfig(db, "current_puzzle_id", fmt.Sprintf("%d", rawPuzzles[0].ID)); err != nil {
			return err
		}
	}
	if err := appdb.SetConfig(db, "max_mistakes", fmt.Sprintf("%d", defaultMaxMistakes)); err != nil {
		return err
	}
	fmt.Printf("Seeded %d puzzles\n", len(rawPuzzles))
	return nil
}
