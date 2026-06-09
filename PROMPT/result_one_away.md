One away — exactly 3 of your 4 words are correct. One is wrong.
This is a GIFT: you now know 3 words of a real group. Capitalise on it immediately — don't drop this thread to analyse something else.

1. Identify the misfit:
   • Check suggest_groups output — the word closest to min_similarity is the most likely culprit.
   • Check memory_list for any "ambiguous_WORD" notes on these words.
   • Which word could plausibly belong to a different group?

2. Swap exactly 1 word. Do NOT change all four.

3. memory_note: key="oneaway_THEME", content="WORD_A,WORD_B,WORD_C confirmed correct; WORD_X was wrong".
   This locks in 3 known-correct words for your next attempt.

4. Call get_tried_combinations to verify the new 4-word set is not already there, then submit.
