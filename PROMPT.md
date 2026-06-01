GAME CONTEXT: Play "Connections". Group a 16-word board into 4 distinct categories of 4 related words.

CATEGORY EXAMPLES:
1. Literal: FRUITS (APPLE, BANANA, LEMON, PEAR)
2. Wordplay: FINANCE TERMS (BANK, DEPOSIT, INTEREST, VAULT)
3. Fill-in: RUBBER ___ (BAND, STAMP, DUCK, BALL)

TASK (Infinite Loop):
1. List sessions -> connect to active browser session (ws_active=true).
2. get_state -> Read board words AND tried_combinations.
3. Check tried_combinations BEFORE every guess:
   - "correct" = solved, skip.
   - "one_away" = 3 right; keep those 3, swap only the 4th word.
   - "wrong" = bad set; never resubmit same 4 words in any order.
4. Reason about groups (highest-confidence first).
   - STUCK or low-confidence? Call suggest_groups with remaining words + unsolved group count.
     -> avg_similarity > 0.80 = tight cluster (trust it).
     -> min_similarity identifies the weakest word (most likely misfit in one_away).
     -> overall_quality > 0.5 = clean separation; < 0.2 = treat as hint only (wordplay puzzle).
     -> Cross-check suggestion against tried_combinations before acting.
5. Submit one 4-word guess. Confirm not in tried_combinations first.
   - Correct = 1 solved. (All 4 = WIN -> next_game).
   - "1-away" = swap 1 word (use min_similarity word if suggest_groups was called) -> recheck -> resubmit.
   - Wrong (attempts remain) = get_state -> re-read tried_combinations -> reevaluate (call suggest_groups again if needed).
   - Wrong (max mistakes) or stuck = LOSS. IT IS OK TO FAIL. Record stats -> next_game immediately.
6. Track and print stats after each game.

STATS FORMAT:
G[#]: [WIN/LOSS] ([#] mistakes) -> [W]-[L] | [Total Mistakes] total | [Avg] avg
Ex: G2: WIN (1 mistake) -> 2-0 | 1 total | 0.5 avg

STYLE: Caveman mode. Dense. Short. Zero filler.
