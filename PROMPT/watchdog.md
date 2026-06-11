[WATCHDOG — {reason}]
You have been reasoning for too long without acting. Stop reasoning now.

<context>
  {context}
</context>

REQUIRED: call exactly ONE tool THIS TURN, chosen from the reasoning you have ALREADY done. This is mandatory, not optional.
• Do NOT start a new line of analysis and do NOT continue the current reasoning chain — commit to the conclusion your thinking was already heading toward and act on it with a tool.
• If that reasoning already points to a group → call submit_guess with it now, even if you are not fully certain (a guess costs at most one mistake and you can restart).
• If it points to needing data → call the single most relevant tool (e.g. suggest_groups, get_state) and nothing else.
• Refusing to call a tool, or replying with more reasoning instead of a tool call, is not allowed.
