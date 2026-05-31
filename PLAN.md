# NYC Connections Mockup — Build Plan

## Stack

| Layer | Technology | Version |
|-------|-----------|---------|
| Backend language | Go | 1.26.3 |
| HTTP router | go-chi/chi | v5 |
| WebSocket | gorilla/websocket | v1 |
| Database | SQLite (modernc.org/sqlite) | latest |
| MCP server | mark3labs/mcp-go | v0.49.0 |
| Frontend framework | Vue | 3.5.35 |
| Build tool | Vite | 8.0.14 |
| State management | Pinia | 3.0.4 |
| CSS | Tailwind CSS | 4.3.0 |
| Runtime manager | mise | LTS |
| Node.js | Node (via mise) | LTS (22.x) |

---

## Project Structure

```
nyc-connections-mockup/
├── .mise.toml
├── PLAN.md
├── backend/
│   ├── go.mod
│   ├── go.sum
│   ├── cmd/server/main.go          # entrypoint: HTTP + MCP servers
│   └── internal/
│       ├── config/config.go        # env vars (port, max_mistakes, db path)
│       ├── db/db.go                # SQLite init + migrations
│       ├── fetcher/fetcher.go      # pulls JSON from GitHub, seeds DB
│       ├── game/
│       │   ├── logic.go            # guess validation, state transitions
│       │   └── session.go          # session CRUD
│       ├── api/
│       │   ├── handlers.go         # REST handlers
│       │   └── ws.go               # WebSocket hub + broadcast
│       └── mcp/server.go           # MCP tool definitions
├── frontend/
│   ├── package.json
│   ├── vite.config.ts
│   ├── tailwind.config.ts
│   ├── index.html
│   └── src/
│       ├── main.ts
│       ├── App.vue
│       ├── types/game.ts           # shared TS types
│       ├── stores/game.ts          # Pinia store
│       ├── composables/useWS.ts    # WebSocket composable
│       └── components/
│           ├── GameBoard.vue       # grid of tiles
│           ├── Tile.vue            # single word tile
│           ├── SolvedGroup.vue     # revealed category banner
│           ├── MistakeDots.vue     # remaining mistakes indicator
│           └── Controls.vue        # Shuffle / Deselect All / Submit
└── tests/
    ├── backend/game_test.go        # Go unit tests
    └── frontend/game.spec.ts       # Vitest component tests
```

---

## Data Source

GitHub repo: `https://github.com/Eyefyre/NYT-Connections-Answers`

Raw JSON: `https://raw.githubusercontent.com/Eyefyre/NYT-Connections-Answers/main/connections.json`

Schema (per puzzle):
```json
{
  "date": "October 17, 2023",
  "id": 1,
  "categories": [
    {
      "title": "THINGS THAT ARE YELLOW",
      "difficulty": 0,
      "cards": [
        { "content": "BUTTER", "position": 0 }
      ]
    }
  ]
}
```

Difficulty maps to color: `0=yellow, 1=green, 2=blue, 3=purple`

---

## Database Schema (SQLite)

```sql
CREATE TABLE puzzles (
  id          INTEGER PRIMARY KEY,
  date        TEXT NOT NULL,
  data        TEXT NOT NULL,   -- full puzzle JSON blob
  played      INTEGER DEFAULT 0
);

CREATE TABLE config (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
-- default rows: current_puzzle_id, max_mistakes (default 4)

CREATE TABLE sessions (
  id             TEXT PRIMARY KEY,   -- UUID
  puzzle_id      INTEGER NOT NULL,
  state          TEXT NOT NULL,      -- JSON: selected, solved, mistakes_left
  created_at     INTEGER NOT NULL,
  updated_at     INTEGER NOT NULL,
  FOREIGN KEY(puzzle_id) REFERENCES puzzles(id)
);
```

---

## mise Setup

`.mise.toml`:
```toml
[tools]
go = "1.26.3"
node = "lts"
```

---

## REST API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/puzzle/current` | Current puzzle (no answers) |
| POST | `/api/session` | Create new game session → returns session_id |
| GET | `/api/session/:id` | Full session state |
| POST | `/api/session/:id/guess` | Submit 4 words |
| PUT | `/api/config/max_mistakes` | Adjust mistake count |
| GET | `/ws?session=:id` | WebSocket upgrade |

`POST /api/session/:id/guess` body:
```json
{ "words": ["JACK", "SOAK", "POCKET", "SPINE"] }
```

Response:
```json
{
  "correct": true,
  "category": { "title": "PIRATE ___", "difficulty": 2, "color": "blue" },
  "mistakes_left": 4,
  "game_over": false,
  "won": false
}
```

---

## WebSocket Events (server → client)

All events are JSON with a `type` field:

```json
{ "type": "guess_result", "payload": { ...same as guess response... } }
{ "type": "game_complete", "payload": { "won": true } }
{ "type": "state_sync",   "payload": { ...full game state... } }
```

`state_sync` fires when MCP tool changes game state — LLM action → UI reacts in real time.

---

## MCP Server

Transport: **SSE** (HTTP-based, LLM connects to `http://localhost:8080/mcp/sse`)

Tools exposed:

| Tool | Args | Description |
|------|------|-------------|
| `get_board` | `session_id` | Returns shuffled word grid + solved groups |
| `get_state` | `session_id` | Full state: mistakes left, selections, solved |
| `submit_guess` | `session_id, words[4]` | Submits guess, triggers WS broadcast |
| `new_game` | — | Advances to next puzzle, creates session |
| `set_max_mistakes` | `count` | Adjusts difficulty (1–10) |

After `submit_guess`, server broadcasts `guess_result` via WebSocket so UI animates immediately.

---

## Game Logic Rules

- 16 words, 4 hidden groups of 4
- Select exactly 4 → Submit
- Correct: group revealed as colored banner (yellow → green → blue → purple), tiles removed from grid
- Wrong: mistakes_left decrements, tiles shake, selection clears
- `max_mistakes` configurable (default 4), stored in `config` table
- Game over when mistakes_left = 0 (loss) or all 4 groups solved (win)
- On win/loss: auto-load next puzzle in DB (advance `current_puzzle_id`)
- "One away" hint: if 3 of 4 selected match a group, surface message

---

## Build Steps

### Step 1 — Environment

```bash
mise install
go version    # should print go1.26.3
node --version # should print v22.x.x
```

### Step 2 — Backend init

```bash
mkdir -p backend/cmd/server backend/internal/{config,db,fetcher,game,api,mcp}
cd backend
go mod init github.com/yourname/connections-backend
go get github.com/go-chi/chi/v5
go get github.com/gorilla/websocket
go get modernc.org/sqlite
go get github.com/mark3labs/mcp-go@v0.49.0
go get github.com/google/uuid
```

### Step 3 — DB + Migrations

Implement `internal/db/db.go`:
- Open SQLite at path from env `DB_PATH` (default `./connections.db`)
- Run `CREATE TABLE IF NOT EXISTS` migrations on startup

### Step 4 — Data Fetcher

Implement `internal/fetcher/fetcher.go`:
- On startup, if `puzzles` table empty: fetch JSON from GitHub raw URL
- Decode array, insert all puzzles into DB
- Set `current_puzzle_id = 1` in config

### Step 5 — Game Logic

Implement `internal/game/logic.go`:
- `ValidateGuess(words []string, puzzle Puzzle) (correct bool, category Category)`
- `AdvancePuzzle(db)` — increment current_puzzle_id

Implement `internal/game/session.go`:
- `CreateSession(puzzleID) Session`
- `GetSession(id) Session`
- `ApplyGuess(session, result) Session`

### Step 6 — WebSocket Hub

Implement `internal/api/ws.go`:
- Hub with map of session_id → connected clients
- `Broadcast(sessionID, event)` called after every guess
- Client connects with `GET /ws?session=<id>`

### Step 7 — HTTP Handlers

Implement `internal/api/handlers.go`:
- Wire all REST endpoints
- After guess: call `ws.Broadcast()`

### Step 8 — MCP Server

Implement `internal/mcp/server.go`:
- Create `mcp.NewServer()`
- Register tools using `mark3labs/mcp-go` tool builder API
- Mount SSE transport at `/mcp/sse` via chi router
- `submit_guess` tool calls same game logic as REST, then broadcasts WS

### Step 9 — Frontend init

```bash
cd ..
npm create vite@latest frontend -- --template vue-ts
cd frontend
npm install
npm install pinia
npm install -D tailwindcss @tailwindcss/vite
```

Configure Tailwind in `vite.config.ts`:
```ts
import tailwindcss from '@tailwindcss/vite'
export default defineConfig({ plugins: [vue(), tailwindcss()] })
```

Add to `src/main.css`:
```css
@import "tailwindcss";
```

### Step 10 — Pinia Store

`src/stores/game.ts` state shape:
```ts
{
  sessionId: string
  puzzle: Puzzle | null
  tiles: Tile[]           // shuffled words
  selected: string[]      // max 4
  solved: SolvedGroup[]
  mistakesLeft: number
  maxMistakes: number
  status: 'idle'|'playing'|'won'|'lost'
  lastResult: GuessResult | null
  shakingTiles: string[]  // tiles to animate on wrong guess
}
```

Actions: `fetchPuzzle`, `toggleTile`, `submitGuess`, `shuffle`, `deselectAll`

### Step 11 — WebSocket Composable

`src/composables/useWS.ts`:
- Connect on session create
- On `guess_result`: update store state + trigger shake/reveal animations
- On `state_sync`: overwrite store (for LLM-driven updates to appear live)
- Auto-reconnect on disconnect

### Step 12 — Components

**`Tile.vue`**
- Props: `word`, `selected`, `shaking`
- Emit: `click`
- Styles: selected = dark gray (like original), shake = CSS keyframe

**`SolvedGroup.vue`**
- Props: `title`, `words`, `color` (0-3)
- Color map: `['bg-yellow-300','bg-green-500','bg-blue-500','bg-purple-600']`

**`MistakeDots.vue`**
- Props: `remaining`, `max`
- Render filled/empty dots

**`GameBoard.vue`**
- Solved groups stacked at top
- Remaining tiles in 4-col grid
- Disable submit if selected.length !== 4

**`Controls.vue`**
- Shuffle (reorder tiles array in store)
- Deselect All (clear selected)
- Submit (disabled when < 4 selected or already submitting)

---

## Running the App

```bash
# backend
cd backend
DB_PATH=./connections.db MAX_MISTAKES=4 PORT=8080 go run ./cmd/server

# frontend (separate terminal)
cd frontend
npm run dev
```

Frontend dev server proxies `/api` and `/ws` to `:8080` via Vite config.

---

## Tests

### Backend — `tests/backend/game_test.go`

```
go test ./...
```

Tests:
- `TestValidGuess` — 4 correct words → correct=true, right category
- `TestWrongGuess` — 4 wrong words → correct=false
- `TestOneAway` — 3 correct + 1 wrong → `one_away=true` in response
- `TestSessionAdvance` — win triggers puzzle ID increment
- `TestMaxMistakesConfig` — set to 2, lose after 2 wrong guesses

### Frontend — `tests/frontend/game.spec.ts`

```bash
cd frontend && npm run test
```

Uses Vitest + `@vue/test-utils`.

Tests:
- Tile toggles selected on click, max 4 enforced
- Submit disabled when < 4 selected
- Correct guess removes tiles, adds SolvedGroup
- Wrong guess decrements mistake dots
- WebSocket `state_sync` event overwrites store

---

## Environment Variables

| Var | Default | Description |
|-----|---------|-------------|
| `PORT` | `8080` | HTTP server port |
| `DB_PATH` | `./connections.db` | SQLite file path |
| `MAX_MISTAKES` | `4` | Default mistake count (1–10) |
| `DATA_URL` | GitHub raw JSON URL | Override data source |
| `MCP_PATH` | `/mcp` | MCP SSE mount path |

---

## LLM Connection (MCP Client Config)

Claude Desktop / any MCP client:
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

LLM calls `new_game` → gets board → calls `submit_guess` in a loop → UI animates each result via WebSocket in real time.
