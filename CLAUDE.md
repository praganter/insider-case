# Smart Campaign Composer

Insider One case study. A marketer writes a campaign brief in natural language; Claude turns it into an audience segment rule and SMS/email copy; a human reviews and approves; messages go through a queue to a mock provider; a dashboard shows delivery status live.
Full plan and decisions: `docs/plan.md`.

## Stack
- Backend: Go (single module), Postgres, Redis + Asynq (queue, retry, scheduled tasks, archive/DLQ)
- Frontend: Vue 3 + Vite + TypeScript, light dependencies
- LLM: Claude via `anthropic-sdk-go`, structured JSON output via `output_config.format` (JSON schema). Forced `tool_choice` is rejected by the current models. The schema guarantees shape only; Go validators enforce the rules (limits, whitelists, depth).
- Live updates: SSE (LLM content stream and dashboard)
- Run: `docker compose up`. Tests: `make test` (runs inside Docker, no local toolchain needed)

## Folder structure
```
backend/
  cmd/api          HTTP + SSE
  cmd/worker       Asynq worker + scheduler
  cmd/mockprovider fake SMS/email provider (errors, delays, 429 via env)
  cmd/seed         migrations + seed data (idempotent)
  internal/segment DSL types, validator, SQL builder
  internal/llm     Client interface, Anthropic impl, mock impl, prompts
  internal/content template render ({{first_name}}), channel limits
  internal/campaign service + status state machine
  internal/sender  task handler, retry classification, Redis rate limit
  internal/store   pgx queries
  migrations/
frontend/          Vue app
docs/              plan.md
ai-sessions/       sanitized Claude session exports
.github/workflows/ CI
```

## Hard rules
1. **The LLM never produces SQL.** It only returns a JSON segment rule. Go validates it against whitelists (fields, operators, events, value types, depth <= 3, conditions <= 20) and builds parameterized SQL from fixed fragments. Values always go through `$n` parameters.
2. **Tests never need a real API key.** All LLM access goes through the `llm.Client` interface.
   - The mock LLM is a first-class mode, not only a test helper: rule-based, produces a valid segment rule and template copy from the brief, streams it with small delays. `ANTHROPIC_API_KEY` empty means mock mode, and the UI shows a "MOCK" badge.
   - The real Anthropic client is tested against a fake HTTP server (`httptest` + SDK base URL override) that imitates stream events, refusal, max_tokens, and 429/529.
   - With a real key, an API failure is shown to the user with a retry option. Never silently fall back to mock.
3. **Channel consent is always enforced** by the SQL builder, independent of the rule.
4. **No duplicate sends.** `UNIQUE(campaign_id, user_id, channel)` in the database plus an idempotency key to the provider.
5. **Limits count runes, not bytes**: SMS <= 160, email subject <= 80, with personalization values included. Use `utf8.RuneCountInString`.
6. Nothing is sent before explicit human approval.

## Code style
- Simple, explainable code. Prefer stdlib and what is already installed; no new dependency for something a few lines can do.
- No speculative abstractions. Match the surrounding code. Touch only what the task needs.
- Everything in the repo's code is English: identifiers, comments, commit messages, log and error messages.
- Comments explain why, not what.

## Docs
- `README.md` and `DECISIONS.md` are bilingual (English and Turkish), each section in both languages.
- README lists anything unfinished.

## Git
- Small, focused commits with short messages (imperative, one line, optional short body).
- Never skip git hooks (`--no-verify`, `-c core.hooksPath=...`, etc.). If a hook fails, fix the cause.
- Commit only when asked.

## Security and privacy
- `.env` is in `.gitignore`. **Never read, write, edit, print, or commit `.env`.** Only `.env.example` (placeholders, no real values) is touched.
- No API keys or secrets anywhere in the repo, logs, or `ai-sessions/`.
- No local machine information in any file: no absolute local paths, usernames, or personal emails. Use relative paths. Sanitize `ai-sessions/` exports before committing.
