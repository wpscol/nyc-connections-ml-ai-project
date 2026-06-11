New run. Bootstrap in order:

1. memory_list — load any notes (cross-puzzle lessons, leftover solved_N/hunches).
2. list_sessions — join the one with ws_active=true (retry if none yet).
3. get_state — branch on status:
   • playing → read remaining words + tried_combinations, then start probing.
   • lost    → restart_game.
   • won     → next_game (same session continues to the next puzzle — don't start a new game), then get_state on the next puzzle's board.

tried_combinations with correct=true are confirmed groups — treat them as anchors even if not in memory.
