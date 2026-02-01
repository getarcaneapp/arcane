# AI Usage Policy

Arcane has clear rules for AI-assisted contributions:

- **All AI usage in any form must be disclosed.** You must state the tool you used (e.g., Claude Code, Cursor, GitHub Copilot, ChatGPT) along with the extent that the work was AI-assisted.

- **Pull requests created by AI must have been fully verified with human testing.** AI must not create hypothetically correct code that hasn't been tested. You must run the development environment, verify both frontend and backend work correctly, and manually test your changes. You must not allow AI to write code for platforms or environments you don't have access to manually test on.

- **Code must follow Arcane's existing patterns.** Before writing code, read [AGENTS.md](AGENTS.md) for technical guidance. AI-generated code that ignores project conventions (Svelte 5 syntax, service patterns, error handling) will be rejected.

- **Issues and discussions can use AI assistance but must have a full human-in-the-loop.** This means that any content generated with AI must have been reviewed *and edited* by a human before submission. AI is very good at being overly verbose and including noise that distracts from the main point. Humans must do their research and trim this down.

- **No AI-generated media is allowed** (art, images, videos, audio, etc.). Text and code are the only acceptable AI-generated content, per the other rules in this policy.

- **Contributors who ignore this policy will face consequences.** Undisclosed AI usage or suspected AI usage without disclosure will result in PR closure. Repeated violations may result in being blocked from contributing. You've been warned.

These rules apply to all outside contributions. Maintainers are exempt from these rules and may use AI tools at their discretion; they've proven themselves trustworthy to apply good judgment.

## Testing Requirements

Before submitting any AI-assisted contribution, you must:

1. Start the development environment: `./scripts/development/dev.sh start`
2. Access the frontend at http://localhost:3000 and verify it works
3. Verify the backend at http://localhost:3552 responds correctly
4. Test your specific changes manually
5. Ensure no linting errors exist
6. Verify hot reload works for both frontend and backend

## There are Humans Here

Please remember that Arcane is maintained by humans.

Every discussion, issue, and pull request is read and reviewed by humans. It is a point of interaction between people and their work. Approaching this with low-effort, unverified submissions is disrespectful and puts the burden of validation on maintainers who volunteer their time.

In a perfect world, AI would produce high-quality, correct code every time. But that reality depends on the person using the AI. Today, we see too many contributions where AI-generated code hasn't been tested, doesn't follow project patterns, or solves problems that don't exist. Until this improves, we need clear rules to protect maintainer time.

## AI is Welcome Here

Arcane is developed with AI assistance, and maintainers use AI tools productively in their workflow. As a project, we welcome AI as a tool for those who use it responsibly!

**Our reason for this policy is not an anti-AI stance**, but rather a response to the increase in low-quality AI-generated pull requests that don't address real user needs or follow project standards. It's about the quality of contributions, not the tools used to create them.

This section exists to be transparent about the project's use of AI and to clarify that this policy targets contribution quality, not the use of AI tools themselves.

## Technical Guidance for AI Tools

If you're using AI tools to contribute to Arcane, ensure your AI is configured with our coding standards. See [AGENTS.md](AGENTS.md) for architecture patterns, anti-patterns to avoid, and project-specific conventions that will help AI tools generate code that matches our standards.
