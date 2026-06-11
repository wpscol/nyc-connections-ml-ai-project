New game starting. Execute in order:

1. memory_list → read ALL notes BEFORE doing anything else.
   Look for:
   • solved_N entries — confirmed groups from a previous failed attempt of this puzzle.
     If any exist, you will re-submit them immediately after joining the session.
   • red_herring_*, tricky_*, pattern_* — cross-puzzle lessons. Apply them now.
   • oneaway_*, hunch_*, partial_* — mid-game clues still relevant.
   If memory is empty, continue. If it has notes, internalize them before touching the board.

2. list_sessions → join the session with ws_active=true and status=playing.
   No sessions available? Call list_sessions again. Never give up.

3. get_state → check the session STATUS first:
   • status="won"  → this puzzle is already finished. Do NOT analyse it.
     Call next_game to advance, then get_state on the new board. That fresh board is your game.
   • status="lost" → call restart_game to retry the same puzzle.
   • status="playing" → continue to step 4.

4. Now on a playing board, read it: remaining words, mistakes_left, tried_combinations.
   If tried_combinations already has correct=true entries, those words are confirmed solved
   (even if they do not appear in memory). Treat them as anchors.

You are now ready to analyse. Begin.
