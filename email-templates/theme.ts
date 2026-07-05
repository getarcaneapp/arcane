/**
 * Shared design tokens for the email templates.
 *
 * Single source of truth for the palette so retheming is a one-file change.
 * Values are plain hex (no oklch / CSS variables) because email clients don't
 * support those. Colors mirror Arcane's default *dark* app theme — see the
 * published previews in frontend/src/lib/utils/theme.ts and frontend/src/routes/layout.css.
 *
 * Only theming lives here; layout stays table-based in the components/emails.
 */

export const colors = {
	bg: '#24262b', // app --background (dark) — email body
	card: '#31343a', // app --card (dark) — main card
	cardBorder: '#3a3e45', // subtle card edge
	panel: '#2b2e34', // flat inset panels (info / total / breakdown)
	panelBorder: '#3a3e45',
	divider: '#3a3e45',
	textPrimary: '#f5f7fa', // app --foreground (dark) — headings
	textBody: '#c7cbd1', // body copy
	textValue: '#e6e8ec', // info values
	textMuted: '#9298a3', // labels / footer / muted
	accent: '#a855f7', // app --primary (dark) — links, CTA, accents
	success: '#34d399', // reserved for positive metrics only
	warningBg: '#fbbf24',
	warningText: '#3f2d02',
	buttonText: '#ffffff'
};

export const fonts = {
	sans: "'Montserrat', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif",
	mono: "'Geist Mono', ui-monospace, SFMono-Regular, Menlo, Monaco, 'Courier New', monospace"
};

export const radii = {
	card: '14px',
	panel: '10px',
	badge: '8px',
	button: '10px'
};
