# Guidelines for i18n automation and AI agents

Purpose: give clear rules so automated tools and AI (including contributors using Copilot-style agents) keep UI text localised correctly.

1. Always use Paraglide messages
- Do not hardcode user-facing strings in `.svelte` files.
- Use the `m.*()` message accessor (e.g. `{m.common_open_menu()}`) for all visible text, `aria-label`, `title`, and `placeholder` values.
- Add new keys to `frontend/messages/en.json` when introducing new user-facing text.

2. Preserve placeholders and interpolation
- For strings with placeholders, use named placeholders (`{name}`, `{count}`) and pass values through the message call where supported.

3. Exposed/global variables and `paraglide` runtime
- When Paraglide uses `globalVariable` strategies, ensure the variable is populated by the server and that UI text uses `m.*()` rather than reading raw global vars.
- Review `frontend/src/lib/paraglide/runtime.js` for any custom strategies; prefer message keys over ad-hoc global strings.

4. Validator and whitelist/blacklist
- Use `node scripts/validate_i18n.js` to detect potential hardcoded texts before committing.
- If the validator reports a false positive, add the exact token or phrase to `scripts/i18n-whitelist.txt`.
- Add problematic strings that must never appear (e.g. secrets, tokens) to `scripts/i18n-blacklist.txt`.

5. Crowdin / translations
- `frontend/messages/en.json` is the source of truth for Crowdin. When adding keys, keep the file tidy and follow existing naming conventions (`section_key_description` / `common_*`).
- Do not edit translated files directly; changes should originate from `en.json` and be pushed to Crowdin via the repo's existing workflow.

6. PR checklist for contributors/agents
- Run `pnpm run validate:i18n` (or `node scripts/validate_i18n.js`) and fix reported issues.
- Add any new keys to `frontend/messages/en.json` with clear descriptions where needed.
- Keep the change small and focused per PR (e.g. one area or component set at a time).

7. AI agent rules (for bots and assistants)
- Never introduce a new user-visible string directly in UI code; always create a message key in `frontend/messages/en.json` and reference it via `m.*()`.
- When modifying components, prefer existing keys where semantically appropriate; do not create duplicate keys.
- When the validator flags a line, explain context in the commit/PR description and either fix or add a whitelist entry.
- Avoid suggesting translations; leave that to Crowdin/localization team.

8. Testing
- Run the validator locally. Optionally run `pnpm` scripts and formatters according to the repo's contribution guide.
- Sanity-check the UI to ensure no placeholders or formatting issues were introduced.

If you are an automated agent, include this file path `docs/i18n-guidelines-for-automation.md` in your PR description and reference the exact changes you made to demonstrate compliance.
