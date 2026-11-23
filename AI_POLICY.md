# AI Usage Policy

Arcane is built with AI assistance and welcomes responsible AI use. This policy is about contribution quality, not banning tools.

> [!IMPORTANT]
> These rules apply to all outside contributions. Undisclosed AI usage (or suspected undisclosed usage) results in PR closure, and repeated violations may result in being blocked from contributing. Maintainers are exempt and use AI tools at their own discretion.

## Rules

- Disclose all AI usage. State the tool/model you used and how much of the work was AI-assisted.
- Manually test every PR yourself. Never submit untested or "should work" AI code, and do not submit code for platforms or environments you cannot test.
- Follow Arcane's conventions in [AGENTS.md](AGENTS.md). AI-generated code that ignores project patterns (Svelte 5 runes, service layers, error handling, etc.) will be rejected.
- Issues and discussions may use AI, but a human must review and trim them before posting. Cut the fluff and keep it focused.
- No AI-generated media (images, art, videos, audio). Only text and code are allowed.

## Testing before submitting

- Start the dev environment with `./scripts/development/dev.sh start` (or `just dev docker`).
- Verify the frontend (`http://localhost:3000`), backend (`http://localhost:3552`), and hot reload all work.
- Manually test your changes and include clear verification evidence (such as before/after screenshots or video for UI changes, or runtime/CLI output).
- Run `just format all` and `just lint all` and fix any errors.

## Why this exists

- Every issue, discussion, and PR is reviewed by human maintainers volunteering their time.
- Low-effort, unverified AI submissions shift the work of validation onto maintainers and slow everyone down.
- If you use AI to contribute, configure it with [AGENTS.md](AGENTS.md) so its output matches Arcane's standards.
