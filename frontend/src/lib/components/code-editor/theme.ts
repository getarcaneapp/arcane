import { tags as t } from '@lezer/highlight';
import { createTheme, type CreateThemeOptions } from '@uiw/codemirror-themes';

function getAccentColor(): string {
	const primaryColor = getComputedStyle(document.documentElement).getPropertyValue('--primary').trim();

	return primaryColor || 'oklch(0.606 0.25 292.717)';
}

function getAccentColorWithAlpha(alpha: number): string {
	const accentColor = getAccentColor();

	if (accentColor.startsWith('oklch')) {
		const hasAlpha = accentColor.includes('/');
		if (hasAlpha) {
			return accentColor.replace(/\/\s*[\d.]+\s*\)/, ` / ${alpha})`);
		}
		return accentColor.replace(')', ` / ${alpha})`);
	}

	if (accentColor.startsWith('#')) {
		const r = parseInt(accentColor.slice(1, 3), 16);
		const g = parseInt(accentColor.slice(3, 5), 16);
		const b = parseInt(accentColor.slice(5, 7), 16);
		return `rgba(${r}, ${g}, ${b}, ${alpha})`;
	}

	return accentColor;
}

export const arcaneDarkInit = (options?: Partial<CreateThemeOptions>) => {
	const { theme = 'dark', settings = {}, styles = [] } = options || {};

	const accentColor = getAccentColor();
	const accentWithAlpha35 = getAccentColorWithAlpha(0.35);
	const accentWithAlpha15 = getAccentColorWithAlpha(0.15);
	const accentWithAlpha05 = getAccentColorWithAlpha(0.05);

	const dynamicSettings: CreateThemeOptions['settings'] = {
		background: '#0d1117',
		foreground: '#c9d1d9',
		caret: accentColor,
		selection: accentWithAlpha35,
		selectionMatch: accentWithAlpha15,
		lineHighlight: accentWithAlpha05,
		gutterBackground: '#0d1117',
		gutterForeground: '#8b949e',
		gutterActiveForeground: '#c9d1d9',
		gutterBorder: 'transparent',

		fontFamily: '"Geist Mono", ui-monospace, SFMono-Regular, Menlo, Consolas, "Liberation Mono", "Courier New", monospace',
		fontSize: '13px'
	};

	const dynamicStyles: CreateThemeOptions['styles'] = [
		{ tag: [t.comment, t.meta], color: '#8b949e' },
		{ tag: [t.keyword, t.modifier, t.operatorKeyword], color: '#ff7b72' },

		{ tag: [t.typeName, t.namespace, t.number, t.atom, t.bool], color: '#ffa657' },
		{ tag: [t.function(t.variableName), t.labelName], color: accentColor },
		{
			tag: [t.className, t.definition(t.variableName), t.propertyName, t.attributeName],
			color: '#d2a8ff'
		},

		{ tag: [t.variableName, t.name], color: '#e6edf3' },
		{ tag: [t.string, t.inserted, t.regexp, t.special(t.string)], color: '#7ee787' },
		{ tag: [t.operator, t.url, t.link, t.escape], color: '#a5d6ff' },

		{ tag: [t.separator, t.punctuation], color: '#6e7681' },

		{ tag: t.heading, color: '#e6edf3', fontWeight: 'bold' },
		{ tag: t.strong, fontWeight: 'bold' },
		{ tag: t.emphasis, fontStyle: 'italic' },
		{ tag: t.strikethrough, textDecoration: 'line-through' },
		{ tag: t.invalid, color: '#f85149' },
		{ tag: t.link, textDecoration: 'underline' }
	];

	return createTheme({
		theme,
		settings: { ...dynamicSettings, ...settings },
		styles: [...dynamicStyles, ...styles]
	});
};

export const arcaneDark = arcaneDarkInit();
