# CLAUDE.md

Reference for this repo. Read before working. NYT Connections clone + MCP server so an LLM can play while a browser UI animates every move live over WebSocket.

## What it is

- 4×4 board, 16 words, 4 hidden groups of 4. Pick 4 → submit. Correct = group revealed (colored banner), tiles removed. Wrong = lose 1 mistake, tiles shake. Win = all 4 solved; loss = mistakes hit 0.
- Two drivers act on the **same** game session: the human (browser REST) and an LLM (MCP tools). Both go through one shared code path; every change broadcasts over WebSocket so the UI reacts in real time.
- Puzzle archive seeded once from GitHub ([Eyefyre/NYT-Connections-Answers](https://github.com/Eyefyre/NYT-Connections-Answers)) into SQLite.

## Stack

| Layer | Tech | Notes |
|---|---|---|
| Backend | Go 1.25 (`go.mod`; README says 1.26) | std `net/http` |
| Router | go-chi/chi v5 | |
| WebSocket | gorilla/websocket | |
| DB | SQLite via `modernc.org/sqlite` (pure Go, no cgo) | WAL mode, single writer |
| MCP | mark3labs/mcp-go v0.49.0 | SSE transport |
| UUID | google/uuid | session IDs |
| Frontend | Vue 3.5 + `<script setup>` TS | |
| Build | Vite 8 | |
| State | Pinia 3 | |
| CSS | Tailwind 4 (`@tailwindcss/vite`) | |
| Tests | Go `testing`, Vitest, Playwright | |
| Toolchain | mise (`.mise.toml`: go 1.26.3, node lts) | |

## Layout

```
.
├── .mcp.json                  # MCP client config → http://localhost:8080/mcp/sse
├── .mise.toml                 # go + node versions
├── PROMPT/                    # AI-solver prompt set (point at it with -prompt-dir); 11 .md files (ALL required — solver crashes if any is missing/empty)
│   ├── system.md              #   system message: rules + invariants + tool list + <context>-block framing
│   ├── session_start.md       #   FIRST game of the run: list_sessions → memory_list → get_state
│   ├── session_continue.md    #   every later game: resume framing (don't treat a mid-run resume as new)
│   ├── analysis.md            #   before each guess: rank candidates, suggest_groups, pre-flight check
│   ├── result_correct.md      #   after correct guess: save memory, next step
│   ├── result_one_away.md     #   after one_away: swap logic, lock 3 correct words
│   ├── result_wrong.md        #   after wrong guess: reconsideration strategy
│   ├── game_won.md            #   after win: save lessons, delete solved_N, next_game
│   ├── game_lost.md           #   after loss: save solved groups, restart_game
│   ├── restart.md             #   after restart: replay solved_N instantly, then analyse
│   └── watchdog.md            #   overthinking/token-limit interrupt: forces a guess; placeholders {reason}, {context}
├── README.md                  # user docs
├── output/                    # gitignored; CSV results from cmd/solve
├── backend/
│   ├── connections.db*        # SQLite (gitignored; auto-created + seeded on first boot)
│   ├── .air.toml              # `air` live-reload → builds cmd/server to tmp/main
│   ├── cmd/
│   │   ├── server/main.go     # entrypoint: wires config→db→fetcher→hub→server→mcp, mounts chi
│   │   ├── solve/            # standalone solver agent: infinite game loop via MCP + OpenAI API
│   │   │   ├── main.go       #   flags, setup, the outer game loop + TUI wiring
│   │   │   ├── runner.go     #   runGame: the per-turn stage machine (sole context-injection point)
│   │   │   ├── prompts.go    #   Stage enum + stageRegistry + loadPrompts (single source of truth)
│   │   │   ├── gamectx.go    #   gameCtx: facts + thinking buffer → <context> block builder
│   │   │   ├── completion.go #   complete(): streaming/non-streaming call, captures reasoning
│   │   │   ├── tools.go      #   callTool, convertTools, resolveModel, log helpers
│   │   │   ├── watchdog.go   #   per-call token/time watchdog + repetition guard
│   │   │   ├── stats.go      #   gameResult, sessionStats
│   │   │   └── tui.go        #   bubbletea split-pane TUI
│   │   └── mcpdemo/main.go    # e2e helper: solves WS-active session by reading answers from DB
│   └── internal/
│       ├── config/config.go   # env vars → Config struct
│       ├── db/db.go           # Open + migrate (3 tables) + GetConfig/SetConfig
│       ├── fetcher/fetcher.go # SeedIfEmpty: GitHub JSON → game.Puzzle → puzzles table
│       ├── embeddings/        # semantic clustering (no game logic dependency)
│       │   ├── client.go      # OpenAI-compatible HTTP embeddings client (LM Studio)
│       │   └── cluster.go     # K-means++ + silhouette stats → SuggestGroups()
│       ├── game/              # pure domain logic (no HTTP)
│       │   ├── types.go       # Card, Category, Puzzle, GameState, GuessResult, stats, WSEvent
│       │   ├── session.go     # Create/Get/Save/Reset session, ApplyGuess (mutates state)
│       │   ├── logic.go       # ValidateGuess, AllWords (shuffle), ComputeStats, RemoveWords
│       │   └── logic_test.go  # unit tests
│       ├── api/
│       │   ├── handlers.go    # Server: REST handlers + exported methods reused by MCP
│       │   └── ws.go          # Hub: per-session client sets, Broadcast / BroadcastAll
│       └── mcp/server.go      # Build(): registers MCP tools, exports MemoryStore for REST
└── frontend/
    └── src/
        ├── main.ts            # createApp + Pinia
        ├── App.vue            # shell: init session, mount GameBoard + SessionSelector + MemoryViewer
        ├── types/game.ts      # TS mirror of backend JSON + color/source maps + MemNote/MemoryPage
        ├── stores/game.ts     # Pinia store: all game state + actions + WS event handler
        ├── composables/useWS.ts  # WebSocket connect/reconnect, dispatches to store
        └── components/
            ├── GameBoard.vue      # composes solved groups, tile grid, controls, logs, stats
            ├── Tile.vue           # one word; selected/shaking/guessing/disabled visual states
            ├── SolvedGroup.vue    # colored revealed-category banner
            ├── MistakeDots.vue    # remaining-mistakes dots
            ├── Controls.vue       # Shuffle / Deselect All / Submit
            ├── SessionSelector.vue# dropdown: switch/delete/clear sessions, status emoji
            ├── MemoryViewer.vue   # 🧠 dropdown: paginated AI memory notes, polls every 4s
            ├── AttemptsLog.vue    # live newest-first log of every guess + source
            └── StatsPanel.vue     # end-of-game modal: win/loss, accuracy, ✓/~/✗ recap
```

## Data model

**SQLite tables** (`internal/db/db.go`):
- `puzzles(id, date, data, played)` — `data` is a full `game.Puzzle` JSON blob (with answers).
- `config(key, value)` — keys: `current_puzzle_id`, `max_mistakes`.
- `sessions(id UUID, puzzle_id, state, created_at, updated_at)` — `state` is a `game.GameState` JSON blob; FK → puzzles.

**Seeding** (`fetcher.go`): on boot, if `puzzles` empty, GETs `DATA_URL`. Raw GitHub schema uses `answers[].{level,group,members}`; fetcher converts to internal `Puzzle{Categories[].{Title,Difficulty,Cards}}` and stores. Sets `current_puzzle_id` = first puzzle, `max_mistakes` = default.

**`GameState` fields** (stored as JSON blob in `sessions.state`):
- `puzzle_id`, `mistakes_left`, `max_mistakes`, `solved`, `remaining_words`, `status`, `guesses` — current-game data.
- `prior_guesses []GuessAttempt` — guess history carried over from previous restarts of the **same** puzzle. Wiped when navigating to a new puzzle (`next`/`prev`). Both `guesses` and `prior_guesses` are merged by `GetGuesses()` and surfaced in `get_state`/`get_board` (and the REST `/guesses` endpoint) so the model sees full history across restarts.

**Difficulty → color**: `0 yellow, 1 green, 2 blue, 3 purple`. Maps live in `types/game.ts` (`DIFFICULTY_COLORS`, `DIFFICULTY_HEX`).

**Group identity**: a solved group is keyed by its `difficulty` level. `ValidateGuess` skips already-solved difficulties; correct = all 4 submitted words match one unsolved category, one-away = exactly 3 match.

## Backend flow

`main.go`: `config.Load()` → `db.Open()` (migrate) → `fetcher.SeedIfEmpty()` → `api.NewHub()` → `api.NewServer(db, hub)` → `mcp.Build(srv, hub, baseURL, embedClient)` → returns `(mcpHandler, memStore)`. chi mounts `srv.Router()` at `/`, MCP SSE handler at `/mcp`, and `memoriesHandler(memStore)` at `GET /api/memories`. Listens on `:PORT`.

**`api.Server`** holds `db` + `hub`. The unexported `processGuess` / `loadCurrentPuzzle` / etc. are the real logic; exported PascalCase wrappers (`ProcessGuess`, `CreateSession`, `GetSession`, `GetGuesses`, `RestartSession`, `NextSession`, `PrevSession`, `SetMaxMistakes`, `ListSessions`, `BroadcastIfComplete`, …) exist **so the MCP server can reuse the exact same path** — it depends on the `gameAPI` interface in `mcp/server.go`, not the concrete type.

`GetGuesses(sessionID)` merges `state.PriorGuesses + state.Guesses` into a single flat slice — the canonical full history for a session across all restarts.

**Guess path** (REST `submitGuess` and MCP `submit_guess` both call `processGuess`):
1. Load session; reject if not `playing`.
2. `ValidateGuess` against the puzzle, skipping solved difficulties.
3. `ApplyGuess` mutates state (decrement mistakes or append solved group + remove words; set `won`/`lost`).
4. Append a `GuessAttempt` to the live log (records `source`: player/mcp/api, difficulty, one_away, timestamp).
5. `SaveSession`.
6. Caller broadcasts `guess_result`, then `broadcastIfComplete` may broadcast `game_complete` with stats.

**Source tagging**: REST reads `X-Source` header (browser sends `player`, defaults to `api`); MCP passes `mcp`.

**Puzzle navigation**: `next`/`prev` wrap around (`nextPuzzleID`/`prevPuzzleID`) and also advance the global `current_puzzle_id` pointer so freshly created sessions follow along. `restart` replays the same puzzle. All call `game.ResetSession(db, id, puzzle, maxMistakes, carryGuesses)` where `carryGuesses` is the merged history for restart (preserving cross-restart memory) and `nil` for next/prev (wipes history). All broadcast `session_reset`.

## WebSocket (`api/ws.go`)

`Hub` = `map[sessionID]→set of clients` guarded by RWMutex. Client connects `GET /ws?session=<id>`; `readPump` blocks until disconnect (drives unregister), `writePump` drains a 32-buffered send channel (non-blocking — drops if full). `ActiveSessions()` powers the `ws_active` flag in session listings (lets an LLM find the session a human is watching).

**Event types** (server→client, `{type, payload}`):
- `guess_result` — a `GuessResult` (includes `guessed` words + `source`).
- `game_complete` — `{won, solved, stats}`.
- `session_reset` — full new state after restart/next/prev.
- `state_sync` — `{session_id}` only; tells UI to re-`GET` the session (fired by MCP `get_state`).
- `config_update` — `{max_mistakes}` (broadcast to ALL sessions via `BroadcastAll`).

## REST API

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/puzzle/current` | current puzzle words (shuffled, no answers) |
| GET | `/api/sessions` | up to 20 recent sessions + `ws_active` |
| DELETE | `/api/sessions` | delete all sessions |
| POST | `/api/session` | create session on current puzzle → state |
| GET | `/api/session/{id}` | full session state |
| DELETE | `/api/session/{id}` | delete one session |
| POST | `/api/session/{id}/guess` | body `{words:[4]}`; honors `X-Source` header |
| POST | `/api/session/{id}/restart` | replay same puzzle |
| POST | `/api/session/{id}/next` / `/prev` | move puzzle (wraps) |
| PUT | `/api/config/max_mistakes` | body `{count}` 1–10 |
| GET | `/api/session/{id}/guesses` | merged full guess history (prior + current) |
| GET | `/api/memories?page=N&per_page=N` | paginated AI memory notes (newest first) |
| GET | `/ws?session={id}` | WebSocket upgrade |

CORS is wide open (`*`). All JSON.

`/api/memories` response: `{ notes: [{key, content, updated_at}], total, page, per_page, pages }`. Backed by the in-process `mcp.MemoryStore`; empty until the solver agent writes notes.

## MCP server (`internal/mcp/server.go`)

SSE at `http://localhost:8080/mcp/sse`. `Build(srv, hub, baseURL, embedClient) (http.Handler, *MemoryStore)` registers tools and returns both the SSE handler and the exported `MemoryStore` so `main.go` can expose it over REST.

**Tools** (no `new_game` — the agent can only join existing sessions):
| Tool | Purpose |
|---|---|
| `list_sessions` | list existing sessions; join `ws_active=true` preferred |
| `get_state` | full state + tried_combinations; fires `state_sync` WS event |
| `get_board` | lightweight board view (no mistakes/status); includes `tried_combinations` |
| `submit_guess` | 4 comma-separated UPPERCASED words; broadcasts `guess_result` + completion |
| `restart_game` | reset same puzzle; carries prior_guesses forward |
| `next_game` | advance to next puzzle (call only after winning) |
| `prev_game` | go back to previous puzzle |
| `suggest_groups` | semantic K-means++ clustering of remaining words |
| `memory_note` | write/overwrite a key→content note in `MemoryStore` |
| `memory_list` | read all notes |
| `memory_delete` | delete one note by key |
| `memory_clear` | wipe all notes (called automatically by solver at startup) |

Every tool returns JSON text with a `next_step`/`tip` hint. `MemoryStore` is an in-process map guarded by a mutex; notes survive across games within one server run and are wiped on solver restart.

## Solver agent (`cmd/solve/main.go`)

Standalone Go binary that plays Connections infinitely via MCP + any OpenAI-compatible API (LM Studio default).

```bash
cd backend
go run ./cmd/solve                               # infinite, auto-detect model
go run ./cmd/solve -rounds 5                     # stop after 5 games
go run ./cmd/solve -model gemma-4                # explicit model name
go run ./cmd/solve -base-url http://host:1234/v1
go run ./cmd/solve -max-tokens 2048              # cap tokens per response
go run ./cmd/solve -csv ../output/my.csv         # custom CSV path (default: ../output/results.csv)
go run ./cmd/solve -prompt-dir ../PROMPT             # choose a prompt set (default: cfg.PromptDir)
```

**Stage-driven prompt injection** — the solver injects exactly ONE focused prompt per relevant game event, and **nothing else is ever injected** (no periodic reminders, no progress snapshots). Each prompt lives in the prompt directory as a `.md` file (11 total). `loadPrompts(dir)` ([prompts.go](backend/cmd/solve/prompts.go)) reads them at startup, **before any network connection**, and `log.Fatalf`s if any file is missing or empty — there are no hardcoded prompt strings. `stageRegistry` is the single source of truth mapping each `Stage` → `{file, priority}`; to add a stage you add an enum constant, a registry row, and a `.md` file. `runGame()` ([runner.go](backend/cmd/solve/runner.go)) sets a pending stage whenever an event fires and injects it on the next turn:

| Trigger | Stage / file injected | Priority |
|---|---|---|
| First game of the run | `session_start.md` | bootstrap (4) |
| Every later game (resume) | `session_continue.md` (avoids "new game" framing on a mid-run resume) | bootstrap (4) |
| `get_state` / `get_board` called | `analysis.md` | analysis (1) |
| `submit_guess` → correct, still playing | `result_correct.md` | result (3) |
| `submit_guess` → one_away | `result_one_away.md` | result (3) |
| `submit_guess` → wrong | `result_wrong.md` | result (3) |
| `submit_guess` → status=won | `game_won.md` | result (3) |
| `submit_guess` → status=lost | `game_lost.md` | result (3) |
| `restart_game` called | `restart.md` | restart (2) |

When several events fire in one turn the higher priority wins (bootstrap > result > restart > analysis). `system.md` is the system message (loaded once, not a stage). `watchdog.md` is a non-stage **interrupt** fired when the watchdog cancels an overthinking call — it `{reason}`/`{context}` placeholders are filled via `strings.NewReplacer`, with `{context}` = `gctx.factsSummary()` (the thinking-free state snapshot, so the interrupt doesn't replay the very reasoning that caused the overthink).

**The `<context>` block** — every injected stage prompt is prefixed with a `<context>…</context>` reference block built by `gameCtx.contextBlock()` ([gamectx.go](backend/cmd/solve/gamectx.go)). It is the ONLY context the model gets beyond the stage prompt itself, and contains: the current **session id**, **mistakes left**, the game's **guesses so far** (solved ✓ / one-away ~ / wrong ✗), **words remaining**, the **memory keys present** (so the model can `memory_list` to read contents on demand), and the **last ~5000 words of the model's own thinking** (`reasoning_content` + content — the chat history deliberately drops `reasoning_content`, so this rolling buffer is the only place it survives across turns). The block is framed as reference material, not a new instruction. `gameCtx.update()` folds every tool result into this state; `reset()` (on `restart_game`) clears board guesses but preserves thinking, memory keys, and session id.

**Only a win ends `runGame`**: `game_won.md` is injected and the function returns so the outer loop advances to the next puzzle. A **loss is non-terminal** — `game_lost.md` is injected and the loop continues so the model saves memory and calls `restart_game`, replaying the same puzzle in-place until it is solved (or `maxTurns`/watchdog limits abort).

**Other key behaviours:**
- `resolveModel()` skips models with "embed" or "rerank" in the name.
- Wipes `MemoryStore` via `memory_clear` before the model sees anything.
- `noToolStreak` detector: nudge after 1 no-tool turn, abort after 3.
- CSV rows: `game, timestamp, status, mistakes, max_mistakes, turns, session_id`. Header written only on new file.

## Frontend flow

`App.vue` calls `store.init()` (POST a session) on mount. `useWS()` watches `store.sessionId` and (re)connects the WebSocket, auto-reconnecting after 2s while `playing`. Incoming events route through `store.handleWSEvent`.

**Pinia store (`stores/game.ts`)** is the single source of UI truth: `remaining`, `solved`, `selected` (max 4), `mistakesLeft`/`maxMistakes`, `status`, plus animation/log/stats fields (`shakingTiles`, `guessingTiles`, `attempts`, `stats`, `showStats`, `toast`). Actions cover the full REST surface (init, newGame, switch/delete/clearAll sessions, toggleTile, shuffle, deselectAll, submitGuess, restart, next, prev).

**Echo handling**: local REST guesses increment `skipNextWSGuess` so the store ignores the WS echo of its own move (the REST response already updated state). External (MCP/API) guesses are NOT skipped — the store briefly highlights `guessingTiles` (~700ms) then applies the result, so the human sees the AI "thinking" before tiles resolve. Wrong guess → `triggerShake` (500ms). `game_complete` reveals `StatsPanel` after a delay so animations finish.

**`MemoryViewer.vue`**: `🧠 N` button in the header (right of `SessionSelector`). Opens a dropdown showing AI memory notes from `GET /api/memories` — key, content, timestamp. Polls every 4s while open so notes appear live as the solver writes them. Badge count updates even while closed. Pagination: 8 notes per page, prev/next controls appear when `pages > 1`.

## Running

```bash
mise install                        # go + node
cd backend && go run ./cmd/server   # :8080, seeds DB on first boot
cd frontend && npm install && npm run dev   # :5173, proxies /api /ws /mcp → :8080
```

Live reload backend: `air` (uses `.air.toml`).

## Env vars

| Var | Default | |
|---|---|---|
| `PORT` | `8080` | HTTP port |
| `DB_PATH` | `./connections.db` | SQLite file |
| `MAX_MISTAKES` | `4` | default per game (1–10) |
| `DATA_URL` | GitHub raw JSON | puzzle source |
| `EMBED_URL` | `http://localhost:1234` | LM Studio base URL |
| `EMBED_MODEL` | *(empty — auto-discovers first loaded model via `/v1/models`)* | embedding model name; set explicitly to override |
| `EMBED_KEY` | *(empty)* | API key — optional for local servers |

`MCPBasePath` is `/mcp`, hardcoded in config (not env-driven). If `EMBED_URL` is empty, the `suggest_groups` MCP tool starts up but returns an error message explaining how to enable it.

## Tests

- Backend: `cd backend && go test ./...` (`game/logic_test.go` — guess validation, mistakes, win/loss, stats).
- Frontend unit: `cd frontend && npm test` (Vitest; `stores/game.spec.ts` — store reactions incl. WS events; jsdom; excludes `e2e/`).
- E2E: `npm run test:e2e` (Playwright; `e2e/features.spec.ts`, `e2e/ws-realtime.spec.ts`). `cmd/mcpdemo` drives a live session by reading answers from the DB to verify real-time WS/stats behavior.

## Conventions & gotchas

- **One logic path**: never duplicate guess/session logic in REST vs MCP — both must funnel through `api.Server` exported methods → `internal/game`. The `gameAPI` interface is the contract.
- `internal/game` is pure (no `net/http`, no `database/sql` in `logic.go`); session persistence lives in `session.go`.
- Session `state` and puzzle `data` are JSON blobs in SQLite — schema changes to `GameState`/`Puzzle` are backward-incompatible with existing rows; delete `connections.db*` to reseed.
- `db.SetMaxOpenConns(1)` — SQLite single-writer; keep queries cheap.
- TS types in `types/game.ts` mirror Go JSON tags (snake_case). Keep them in sync when changing payloads.
- Max mistakes is set via the REST endpoint `PUT /api/config/max_mistakes` (browser only) and only affects NEW sessions; running sessions keep their count. There is no MCP tool for it — the solver cannot change the mistake limit.
- Solved groups are deduped by `title` on the client and skipped by `difficulty` on the server.
