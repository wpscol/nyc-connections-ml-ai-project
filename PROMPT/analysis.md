FIRST check status from get_state — only act if the game is in play:
• status="won"  → do NOT analyse or guess. Call next_game and stop.
• status="lost" → do NOT analyse or guess. Call restart_game.
• status="playing" → continue below.

ONLY 4 WORDS LEFT? Submit those exact 4 words now. Do NOT analyse — the last group is forced. Skip everything below.

Otherwise pick your next guess. Probe — find groups by testing, not by perfect deduction.

1. Use tried_combinations (shown in get_state):
   • correct → settled group, gone; never re-analyse it, use the rest as anchors.
   • one_away → 3 of those 4 are right; re-analyse only to keep those 3 and swap the weakest 1.
   • wrong → never resubmit any permutation.

2. Rank 3-6 concrete 4-word candidates: literal sets, "___ X" fill-ins, prefix/suffix, wordplay, homophones, double meanings. Stuck? call suggest_groups.

3. Submit:
   • mistakes_left ≥ 2 → fire your top candidate now, even at ~50% confidence.
   • mistakes_left = 1 → your single best group (a loss just restarts with everything saved).

Pre-flight: confirm the 4 words are NOT already in tried_combinations (any order = duplicate).
