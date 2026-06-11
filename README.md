# Connections

NYT Connections clone with an MCP server so an LLM can play while the browser UI animates every move live over WebSocket.

![Go 1.26](https://img.shields.io/badge/Go-1.26-00ADD8) ![Vue 3.5](https://img.shields.io/badge/Vue-3.5-42b883) ![Vite 8](https://img.shields.io/badge/Vite-8-646cff)

## Dependencies

Only one tool is required — [mise](https://mise.jdx.dev/) provisions everything else:

```bash
mise install   # installs Go 1.26.3 + Node LTS (from .mise.toml)
```

That's it. SQLite is pure-Go (`modernc.org/sqlite`, no cgo), puzzle data is fetched from GitHub on first boot. No database, no system libraries to install.

An LLM server is **only** needed for the optional AI features (solver + `suggest_groups`) — see [Models](#models-optional).

## Run

```bash
# 1. backend — http://localhost:8080  (seeds the puzzle DB on first boot)
cd backend && go run ./cmd/server

# 2. frontend — http://localhost:5173  (proxies /api /ws /mcp → :8080)
cd frontend && npm install && npm run dev
```

Open http://localhost:5173 and play. Pick 4 words → Submit.

## LM Studio setup

The game itself needs no model. Two features call an OpenAI-compatible LLM server — [LM Studio](https://lmstudio.ai/) at `http://localhost:1234` by default:

| Feature | Needs | Model |
|---|---|---|
| AI solver (`cmd/solve`) | a **chat model with tool calling** | [`google/gemma-4-12b-qat`](https://lmstudio.ai/models/google/gemma-4-12b-qat) |
| `suggest_groups` MCP tool | an **embedding model** | [`nomic-ai/nomic-embed-text-v1.5`](https://lmstudio.ai/models/nomic-ai/nomic-embed-text-v1.5) |

**1. Install the models** (LM Studio → 🔍 Search, or the CLI):

```bash
lms get google/gemma-4-12b-qat
lms get nomic-ai/nomic-embed-text-v1.5
```

**2. Load them** — open the 📂 *My Models* tab and load both. They can be loaded at the same time; the solver auto-skips the embedding model by name, and `suggest_groups` auto-skips chat models.

**3. Enable the server** — go to the **Developer** tab (`</>`), toggle **Status: Running** (or `lms server start`). It listens on `http://localhost:1234`.

**4. Configure MCP in LM Studio** (optional — lets LM Studio's own chat drive the game). Developer tab → `mcp.json`, add the running game server:

```json
{
  "mcpServers": {
    "connections-game": {
      "type": "sse",
      "url": "http://localhost:8080/mcp/sse"
    }
  }
}
```

If no LLM server is running, human play still works — only the solver and `suggest_groups` are disabled.

### Verify the connection

With the backend running, hit the health check:

```bash
curl http://localhost:8080/api/health
```

```json
{
  "status": "ok",
  "llm": {
    "base_url": "http://localhost:1234",
    "reachable": true,
    "models": ["google/gemma-4-12b-qat", "text-embedding-nomic-embed-text-v1.5"]
  }
}
```

- `status: ok` → backend is up.
- `llm.reachable: true` with your models listed → LM Studio is connected and ready.
- `llm.reachable: false` (with an `error`) → start LM Studio's server / load a model, then retry.

### Watch an LLM play

With the backend + frontend running and the chat model loaded:

```bash
cd backend && go run ./cmd/solve        # plain logs
cd backend && go run ./cmd/solve -tui   # split-pane TUI: live reasoning + game log
```

Open the browser to watch every move animate in real time. Flags: `-rounds N`, `-model NAME`, `-base-url URL`, `-max-tokens N`.

## Custom puzzles

The default puzzle set is seeded from the [Eyefyre/NYT-Connections-Answers](https://github.com/Eyefyre/NYT-Connections-Answers) archive on first boot. To load your own puzzles instead, create a JSON file and point `DATA_URL` at it before the database is created.

**1. Create a JSON file** (e.g. `my-puzzles.json`):

```json
[
  {
    "id": 9001,
    "date": "My Custom Puzzle",
    "answers": [
      {"level": 0, "group": "THINGS IN A KITCHEN",  "members": ["OVEN", "SINK", "FRIDGE", "BLENDER"]},
      {"level": 1, "group": "FAST ANIMALS",         "members": ["CHEETAH", "FALCON", "SAILFISH", "PRONGHORN"]},
      {"level": 2, "group": "___ LIGHT",            "members": ["GREEN", "FLASH", "MOON", "CANDLE"]},
      {"level": 3, "group": "TYPES OF ROCK",        "members": ["IGNEOUS", "SEDIMENTARY", "METAMORPHIC", "PUMICE"]}
    ]
  }
]
```

Rules:
- Each puzzle needs exactly **4 groups**, each with exactly **4 members**.
- `level`: `0` = yellow (easiest) → `3` = purple (hardest).
- `date` is the display name shown in the session list — any string works.
- Use `id` values ≥ 9000 to avoid clashing with the GitHub archive (IDs 1–500).

**2. Delete the existing database and reseed:**

```bash
rm backend/connections.db backend/connections.db-wal backend/connections.db-shm 2>/dev/null; true
cd backend && DATA_URL=/absolute/path/to/my-puzzles.json go run ./cmd/server
```

The server seeds on first boot and then runs normally. `DATA_URL` can be a `file://` path or an `https://` URL.

## Environment variables

| Variable | Default | |
|---|---|---|
| `PORT` | `8080` | HTTP port |
| `DB_PATH` | `./connections.db` | SQLite file |
| `MAX_MISTAKES` | `4` | mistakes per game (1–10) |
| `DATA_URL` | GitHub raw JSON | puzzle source |
| `EMBED_URL` | `http://localhost:1234` | LM Studio base URL (for `suggest_groups`) |
| `EMBED_MODEL` | *(auto-discover)* | embedding model name |
| `EMBED_KEY` | *(empty)* | API key — optional for local servers |

## MCP server

SSE endpoint: `http://localhost:8080/mcp/sse` (also in `.mcp.json`). Point any MCP client at it; the LLM can only join existing sessions, not create them — open the browser first so it has a game to play.

## Tests

```bash
cd backend  && go test ./...   # game logic
cd frontend && npm test        # Pinia store + WS events (Vitest)
```

See [CLAUDE.md](CLAUDE.md) for architecture, the full REST/WebSocket/MCP surface, and the solver internals.

## Architecture decisions

### Core game

- **Single shared logic path** — REST (browser) and MCP (AI) both funnel through the same exported `api.Server` methods → `internal/game`. The `gameAPI` interface is the contract. Zero duplication of guess/session logic between the two entry points.

- **Pure domain layer** — `internal/game/logic.go` has no `net/http`, no `database/sql`. Game rules are plain functions testable in isolation; session persistence lives separately in `session.go`.

- **SQLite as the only store** — Pure-Go driver (`modernc.org/sqlite`, no cgo). Single-writer (`SetMaxOpenConns(1)`), WAL mode. Puzzle data and session state stored as JSON blobs — no ORM, no migrations beyond a 3-table schema.

- **WebSocket Hub** — Per-session set of clients guarded by `RWMutex`. Broadcast is non-blocking (drops if send buffer full). `ActiveSessions()` powers the `ws_active` flag that lets the AI find the session a human is currently watching.

- **Cross-restart guess history** — `GameState` keeps both `guesses` (current run) and `prior_guesses` (carried over from restarts). `GetGuesses()` merges both; restart carries history forward; next/prev wipes it. The AI sees the full attempt history across all restarts of one puzzle.

- **Source tagging on guesses** — Every `GuessAttempt` records `source` (`player`/`mcp`/`api`). REST reads `X-Source` header; MCP hardcodes `"mcp"`. Powers the attempts log and lets humans see which moves came from the AI.

### MCP server (AI ↔ game bridge)

- **MCP over SSE as the AI interface** — Game exposes a Model Context Protocol server at `/mcp/sse`. Any MCP-compatible client (LM Studio, Claude Desktop, custom solver) can play with no changes to the game. The AI can only join existing sessions — it cannot create them.

- **Tool-per-action design** — 13 tools covering the full game surface (`get_state`, `submit_guess`, `restart_game`, `next_game`, `suggest_groups`, memory CRUD, etc.). Each tool response includes a `next_step`/`tip` field so the AI always receives guidance on what to do next.

- **ws_active discovery** — `list_sessions` includes `ws_active=true` for sessions with a live WebSocket client. Both the prompt and the tool description push the AI to prefer these — ensuring the AI plays on the session a human is watching.

### Semantic embedding (`suggest_groups`)

- **K-means++ clustering as a second opinion** — Remaining words are embedded via a local LM Studio model, clustered with K-means++, scored with silhouette coefficient. Returns `avg_similarity`, `min_similarity`, `confidence` per cluster, and `overall_quality`. Explicitly framed as a second opinion, not ground truth — embeddings capture meaning, not wordplay or Purple-difficulty fill-in patterns.

- **Auto-model discovery** — Both the solver and the embedding client auto-discover the first loaded model from `/v1/models`. The solver skips model IDs containing `"embed"` or `"rerank"`; the embedding client picks any remaining model. No config needed for the common case.

### Solver agent (`cmd/solve`)

- **Agentic workload as a standalone Go binary** — `cmd/solve` runs an infinite MCP+LLM loop outside the game server. OpenAI-compatible client speaks to LM Studio at `localhost:1234`. Outputs per-game CSV rows (status, mistakes, turns, session ID) for tracking win rate over time.

- **Stage-based prompt injection** — Instead of one monolithic system prompt, 9 focused files in `PROMPT/` are injected as fresh user messages at the exact moment they are relevant: `session_start.md` on boot, `analysis.md` after `get_tried_combinations`, `result_wrong.md` after a bad guess, etc. The system prompt stays minimal (invariants only); per-event guidance stays focused.

- **Injection priority system** — When multiple events fire in one turn, a priority ladder resolves which stage wins: `priAnalysis=1 < priRestart=2 < priResult=3`. Terminal stages (`won`/`lost`) are injected immediately before `runGame` returns.

- **gameCtx progress tracker** — Per-game struct accumulates solved/one_away/wrong guesses and mistake counts. Injected as a compact structured summary after every `submit_guess` and every 8 turns. Prevents the model from hallucinating already-tried combinations. Cleared on restart; fully wiped on `next_game`.

- **Periodic rules reminder** — Every 30 turns a compact rules reminder is injected as a recovery nudge for long games where context compresses. Not the primary instruction mechanism.

- **Tool-choice mode switching** — `"required"` in plain log mode forces a tool call every turn (prevents reasoning walls with no action). `"auto"` in TUI mode lets the model emit reasoning before picking a tool, which fills the thinking pane.

### Watchdog & safety systems

- **Per-turn watchdog goroutine** — Every model call spawns a `watchdog` monitoring token budget (~2k tokens) and wall-clock timeout (3 minutes). `setFired(reason)` sets the flag before calling `cancel()` — race-safe. On trigger: injects a nudge with current game state and tells the model to call a tool immediately.

- **Consecutive overthink limit** — `overthinksInRow` tracks back-to-back watchdog triggers. After 3 consecutive fires the game is aborted. A successful turn resets the counter to 0.

- **Repetition loop detection** — `looksRepetitive()` checks if the last 160 chars of the accumulated streaming tail are strictly periodic with a unit ≤ 40 chars — the signature of quantized-model degenerate loops (`"000,000,..."`, `"<|channel|>..."`). Operates on the character stream (not token boundaries, which vary). Checked every 4th chunk; fires the watchdog immediately on detection.

- **No-tool streak detector** — If the model produces 3 consecutive turns with no tool calls the game is aborted. After 1 such turn a nudge is injected: `"Stop reasoning. Call a tool now."`.

### AI memory system

- **AI-managed in-process key→value store** — `MemoryStore` is a mutex-guarded map in the MCP package. The AI writes structured notes with `memory_note` (patterns, red herrings, cross-game lessons), reads with `memory_list`, prunes with `memory_delete`, and bulk-wipes with `memory_clear`. Notes survive across games within one server run; wiped automatically at solver startup.

- **Memory as a REST resource** — `MemoryStore` is exported from the MCP package and mounted at `GET /api/memories` with pagination. The `MemoryViewer.vue` component polls every 4s while open, showing AI notes live. Badge count updates even while the dropdown is closed.

### Split-pane TUI (`-tui` flag)

- **bubbletea + lipgloss** — Two-pane terminal UI: game log (left, 40% width) + live reasoning stream (right, 60%) + status bar. Both panes are scrollable; Tab switches focus. Keys: `↑↓/jk` scroll, `pgup/pgdn`, `g/G` top/live-tail, `q` quit.

- **Value-copy constraint** — bubbletea passes the model struct by value on every `Update()`. `strings.Builder` cannot be stored in the model (panic on copy); `thinkBuf` is a plain `string` with `+=`.

- **Reasoning channel separation** — `delta.ReasoningContent` (Gemma/QwQ/R1 chain-of-thought) is streamed to the thinking pane but never added to conversation history. Regular `delta.Content` is both shown and appended. Keeps the context clean; chain-of-thought never pollutes the message history.

- **Log stored as raw text, colored at render** — `logLines` stores raw strings (no ANSI). `logDisplayLines()` wraps each line to pane width first (rune-aware), then applies color per wrapped row. Coloring first would corrupt wrap counts. Scroll offset advances by wrapped row count of each new line to keep the viewport anchored.

- **Layout single source of truth** — `layout()` computes all pane dimensions in one place; both `Update()` (scroll clamping) and `View()` call it to guarantee they agree. Both panes use `MaxWidth`/`MaxHeight` backstops to hard-clip content and prevent lipgloss re-wrapping from corrupting the screen.
