import { tags as t } from '@lezer/highlight';
import { createTheme, type CreateThemeOptions } from '@uiw/codemirror-themes';

function getAccentColor(): string {
	const primary = getComputedStyle(document.documentElement).getPropertyValue('--primary').trim();
	return primary || 'oklch(0.606 0.25 292.717)';
}

function getAccentColorWithAlpha(alpha: number): string {
	const accent = getAccentColor();

	if (accent.startsWith('oklch')) {
		const hasAlpha = accent.includes('/');
		if (hasAlpha) {
			return accent.replace(/\/\s*[\d.]+\s*\)/, ` / ${alpha})`);
		}
		return accent.replace(')', ` / ${alpha})`);
	}

	if (accent.startsWith('#') && accent.length >= 7) {
		const r = parseInt(accent.slice(1, 3), 16);
		const g = parseInt(accent.slice(3, 5), 16);
		const b = parseInt(accent.slice(5, 7), 16);
		return `rgba(${r}, ${g}, ${b}, ${alpha})`;
	}

	return accent;
}

export function arcaneCodeMirrorTheme(options?: Partial<CreateThemeOptions>) {
	const { theme = 'dark', settings = {}, styles = [] } = options || {};

	const accentColor = getAccentColor();
	const accentWithAlpha35 = getAccentColorWithAlpha(0.35);
	const accentWithAlpha15 = getAccentColorWithAlpha(0.15);
	const accentWithAlpha05 = getAccentColorWithAlpha(0.05);

	// Keep backgrounds transparent so the editor inherits the card background.
	const dynamicSettings: CreateThemeOptions['settings'] = {
		background: 'transparent',
		foreground: 'hsl(var(--foreground))',
		caret: accentColor,
		selection: accentWithAlpha35,
		selectionMatch: accentWithAlpha15,
		lineHighlight: accentWithAlpha05,
		gutterBackground: 'transparent',
		gutterForeground: 'hsl(var(--muted-foreground))',
		gutterActiveForeground: 'hsl(var(--foreground))',
		gutterBorder: 'transparent',

		fontFamily:
			'"Geist Mono", ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace'
	};

	// These colors mirror the project's previous CodeMirror implementation to ensure
	// highlighting is clearly visible on mobile.
	const dynamicStyles: CreateThemeOptions['styles'] = [
		{ tag: [t.comment, t.meta], color: 'hsl(var(--muted-foreground))' },
		{ tag: [t.keyword, t.modifier, t.operatorKeyword], color: '#ff7b72' },

		{ tag: [t.typeName, t.namespace, t.number, t.atom, t.bool], color: '#ffa657' },
		{ tag: [t.function(t.variableName), t.labelName], color: accentColor },
		{ tag: [t.className, t.definition(t.variableName), t.propertyName, t.attributeName], color: '#d2a8ff' },

		{ tag: [t.variableName, t.name], color: 'hsl(var(--foreground))' },
		{ tag: [t.string, t.inserted, t.regexp, t.special(t.string)], color: '#7ee787' },
		{ tag: [t.operator, t.url, t.link, t.escape], color: '#a5d6ff' },

		{ tag: [t.separator, t.punctuation], color: 'hsl(var(--muted-foreground))' },

		{ tag: t.heading, color: 'hsl(var(--foreground))', fontWeight: 'bold' },
		{ tag: t.strong, fontWeight: 'bold' },
		{ tag: t.emphasis, fontStyle: 'italic' },
		{ tag: t.strikethrough, textDecoration: 'line-through' },
		{ tag: t.invalid, color: 'hsl(var(--destructive))' },
		{ tag: t.link, textDecoration: 'underline' }
	];

	return createTheme({
		theme,
		settings: { ...dynamicSettings, ...settings },
		styles: [...dynamicStyles, ...styles]
	});
}