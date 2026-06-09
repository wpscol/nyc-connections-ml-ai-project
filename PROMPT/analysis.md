You have the history. Now find the best group to submit.

STEP 1 — Rule out dead ends from tried_combinations:
• correct=true → confirmed group; those words are anchors.
• one_away=true → 3 of those 4 are correct; only 1 was wrong.
• wrong → all 4 span multiple groups; never resubmit any permutation.

STEP 2 — Check memory FIRST. It is your scratchpad of crossed-off and candidate groupings.
Call memory_list and scan for:
• ruled_out_THEME → a grouping you already eliminated; do NOT reconsider it.
• candidate_THEME → a possible grouping you noted but haven't tried; weigh it now.
• oneaway_THEME → you already know 3 correct words for that group; use them.
• partial_THEME → a nearly-complete group you were building.
• ambiguous_WORD → a word that could fit multiple groups; resolve it now.
• hunch_THEME → an unverified idea worth testing.
If memory has a useful note, act on it before calling suggest_groups. Never re-derive what you already worked out.

STEP 3 — Call suggest_groups with the remaining words.
Then brainstorm every possible theme: literal sets, ___ X fill-in, prefix/suffix, wordplay, double meanings.
For each candidate: name the title (≤5 words). Argue against each member. Try swapping words between groups.

As you reason, WRITE IT DOWN — don't hold it all in your head:
• A grouping you eliminate → memory_note(key="ruled_out_THEME", content="W1,W2,W3,W4 — why it fails").
• A plausible grouping you won't submit yet → memory_note(key="candidate_THEME", content="W1,W2,W3,W4 — confidence + reason").
• A word that could fit two groups → memory_note(key="ambiguous_WORD", content="fits THEME-A or THEME-B").
This keeps the search converging instead of looping over the same dead ends.

STEP 4 — Rank by confidence (embedding score + reasoning), then decide using mistakes_left:
• mistakes_left ≥ 2 → DON'T wait for certainty. Submit your best candidate even at ~50-60% confidence.
  A wrong guess returns one_away or wrong — both are FREE INFORMATION that narrows the puzzle.
  one_away means 3 of your 4 were right: you just found a near-complete group. This is the fastest path.
  Probing beats endless re-analysis. Acting on a decent guess > thinking yourself in circles.
• mistakes_left = 1 → LAST chance. Only submit a group you are ≥85% sure about. Solve a safer group first if one exists.
• Special case: if exactly 4 words remain and 3 groups are solved → submit immediately. Last group is forced.

STEP 5 — Pre-flight before submit_guess:
Confirm the exact 4 words are NOT in tried_combinations (any permutation = duplicate).
Unsure? Call get_tried_combinations again. Never guess from memory alone.

Submit now. If you have mistakes to spare, a probe is better than paralysis.
