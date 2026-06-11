Board is reset. Same puzzle, fresh mistakes. Execute in order:

1. memory_list → find all solved_N entries (solved_1, solved_2, …).
   Also look for oneaway_THEME entries — those tell you 3 confirmed words for a group.

2. For each solved_N entry: call submit_guess with those exact 4 words immediately.
   These are confirmed answers — zero analysis, zero mistake risk.
   If memory_list returns nothing but get_tried_combinations shows correct=true entries,
   reconstruct: those words are confirmed solved, re-submit them group by group.

3. After replaying all memorised groups → call get_tried_combinations.
   • wrong entries — all permutations are dead ends, skip entirely.
   • one_away entries — check memory for oneaway_THEME to find the 3 confirmed words.

4. Begin fresh analysis on the remaining unknown words.
   If stuck, call memory_list again — a hunch or partial note may unlock the next group.
