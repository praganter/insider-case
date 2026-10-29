---
name: ship-session
description: Sanitize the latest /export and commit it to ai-sessions/ (arg: NN-name, e.g. 02-skeleton)
---
Ship the session export as `ai-sessions/$ARGUMENTS.txt`.

1. Find the newest `*.txt` in the repo root (the raw `/export`). If there is none, or `$ARGUMENTS` is empty, stop and say so.
2. Run `python .claude/skills/ship-session/scripts/session.py sanitize <raw> ai-sessions/$ARGUMENTS.txt` and show its replacement counts.
3. Delete the raw file from the repo root.
4. `git add ai-sessions/$ARGUMENTS.txt` only, show `git diff --cached --stat`, and ask for confirmation.
5. After confirmation: commit with `Add sanitized $ARGUMENTS session export` and push. The pre-commit hook must pass; never skip it.
