# Connections

NYT Connections clone with an MCP server so LLMs can play — UI reacts in real time via WebSocket.

![Go 1.26](https://img.shields.io/badge/Go-1.26-00ADD8) ![Vue 3.5](https://img.shields.io/badge/Vue-3.5-42b883) ![Vite 8](https://img.shields.io/badge/Vite-8-646cff)

## Features

- Full Connections game: select 4 words, submit, reveal groups with color
- Configurable mistake count (default 4)
- Shuffles to next puzzle automatically on win
- MCP server (SSE) — connect any LLM; moves appear in the UI live
- WebSocket broadcasts all state changes (REST or MCP-driven)
- Puzzle data seeded from [NYT-Connections-Answers](https://github.com/Eyefyre/NYT-Connections-Answers)

## Stack

| | Technology | Version |
|---|---|---|
| Backend | Go | 1.26.3 |
| Router | go-chi | v5 |
| WebSocket | gorilla/websocket | v1 |
| Database | SQLite (modernc.org/sqlite) | — |
| MCP server | mark3labs/mcp-go | v0.49.0 |
| Frontend | Vue | 3.5.35 |
| Build | Vite | 8.0.14 |
| State | Pinia | 3.0.4 |
| CSS | Tailwind CSS | 4.3.0 |

## Requirements

[mise](https://mise.jdx.dev/) manages Go and Node versions.

```bash
mise install   # installs Go 1.26.3 and Node LTS from .mise.toml
```

## Running

**1. Backend**

```bash
cd backend
go run ./cmd/server
```

Server starts on `http://localhost:8080`. On first boot it fetches all puzzles from GitHub and seeds the SQLite database.

**2. Frontend**

```bash
cd frontend
npm install
npm run dev
```

Opens at `http://localhost:5173`. All `/api`, `/ws`, `/mcp` requests are proxied to the backend.

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | HTTP server port |
| `DB_PATH` | `./connections.db` | SQLite file path |
| `MAX_MISTAKES` | `4` | Default mistakes per game (1–10) |
| `DATA_URL` | GitHub raw JSON URL | Override puzzle data source |

```bash
DB_PATH=./data.db MAX_MISTAKES=3 go run ./cmd/server
```

## REST API

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/puzzle/current` | Current puzzle (shuffled words, no answers) |
| `POST` | `/api/session` | Create game session → returns `session_id` |
| `GET` | `/api/session/:id` | Full session state |
| `POST` | `/api/session/:id/guess` | Submit 4 words |
| `PUT` | `/api/config/max_mistakes` | Adjust mistake count |
| `GET` | `/ws?session=:id` | WebSocket connection |

**Submit guess:**
```bash
curl -X POST http://localhost:8080/api/session/<id>/guess \
  -H 'Content-Type: application/json' \
  -d '{"words": ["JACK", "SOAK", "POCKET", "SPINE"]}'
```

```json
{
  "correct": true,
  "category": { "title": "PIRATE ___", "words": [...], "difficulty": 2 },
  "one_away": false,
  "mistakes_left": 4,
  "status": "playing"
}
```

## WebSocket Events

Connect to `ws://localhost:8080/ws?session=<session_id>`. All events are JSON:

```json
{ "type": "guess_result",  "payload": { ...GuessResult } }
{ "type": "game_complete", "payload": { "won": true } }
{ "type": "state_sync",    "payload": { "session_id": "..." } }
```

`state_sync` fires when an MCP tool changes state — allows the UI to refresh from the backend.

## MCP Server (LLM integration)

SSE endpoint: `http://localhost:8080/mcp/sse`

**Claude Desktop** — add to `claude_desktop_config.json`:
```json
{
  "mcpServers": {
    "connections-game": {
      "url": "http://localhost:8080/mcp/sse",
      "transport": "sse"
    }
  }
}
```

**Available tools:**

| Tool | Args | Description |
|---|---|---|
| `get_board` | `session_id` | Current board info |
| `get_state` | `session_id` | Full state + triggers UI sync |
| `submit_guess` | `session_id`, `words` | 4 comma-separated words, e.g. `JACK,SOAK,POCKET,SPINE` |
| `new_game` | — | Info about current puzzle |
| `set_max_mistakes` | `count` | Adjust difficulty (1–10) |

Every `submit_guess` call broadcasts the result over WebSocket — the browser UI animates the move in real time.

**Example LLM session:**
1. `new_game` → get puzzle info
2. `POST /api/session` → get `session_id`
3. `get_state { session_id }` → read the board
4. `submit_guess { session_id, words: "A,B,C,D" }` → see result, watch UI update
5. Repeat until won or lost

## Tests

**Backend** (Go):
```bash
cd backend
go test ./...
```

**Frontend** (Vitest):
```bash
cd frontend
npm test
```

Covers: correct/wrong/one-away guess logic, mistake decrement, win/loss state, tile selection limits, Pinia store reactions to WebSocket events.

## Project Structure

```
.
├── .mise.toml                  # Go 1.26.3 + Node LTS
├── backend/
│   ├── cmd/server/main.go      # entrypoint
│   └── internal/
│       ├── config/             # env vars
│       ├── db/                 # SQLite + migrations
│       ├── fetcher/            # seeds DB from GitHub
│       ├── game/               # logic, session, types
│       ├── api/                # REST handlers + WebSocket hub
│       └── mcp/                # MCP SSE server + tools
└── frontend/
    └── src/
        ├── App.vue
        ├── stores/game.ts      # Pinia store
        ├── composables/useWS.ts
        └── components/
            ├── GameBoard.vue
            ├── Tile.vue
            ├── SolvedGroup.vue
            ├── MistakeDots.vue
            └── Controls.vue
```

## Difficulty Colors

| Color | Difficulty | Meaning |
|---|---|---|
| Yellow | 0 | Easiest |
| Green | 1 | Medium |
| Blue | 2 | Hard |
| Purple | 3 | Hardest |
