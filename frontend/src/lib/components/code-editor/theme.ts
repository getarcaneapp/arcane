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

interface ArcaneThemePalette {
	theme: 'dark' | 'light';
	background: string;
	foreground: string;
	gutterForeground: string;
	comment: string;
	keyword: string;
	typeName: string;
	className: string;
	variableName: string;
	string: string;
	operator: string;
	separator: string;
	invalid: string;
}

function createArcaneThemeInit(palette: ArcaneThemePalette) {
	return (options?: Partial<CreateThemeOptions>) => {
		const { theme = palette.theme, settings = {}, styles = [] } = options || {};

		const accentColor = getAccentColor();
		const accentWithAlpha35 = getAccentColorWithAlpha(0.35);
		const accentWithAlpha15 = getAccentColorWithAlpha(0.15);
		const accentWithAlpha05 = getAccentColorWithAlpha(0.05);

		const dynamicSettings: CreateThemeOptions['settings'] = {
			background: palette.background,
			foreground: palette.foreground,
			caret: accentColor,
			selection: accentWithAlpha35,
			selectionMatch: accentWithAlpha15,
			lineHighlight: accentWithAlpha05,
			gutterBackground: palette.background,
			gutterForeground: palette.gutterForeground,
			gutterActiveForeground: palette.foreground,
			gutterBorder: 'transparent',

			fontFamily: '"Mona Sans Mono", ui-monospace, SFMono-Regular, Menlo, Consolas, "Liberation Mono", "Courier New", monospace',
			fontSize: '13px'
		};

		const dynamicStyles: CreateThemeOptions['styles'] = [
			{ tag: [t.comment, t.meta], color: palette.comment },
			{ tag: [t.keyword, t.modifier, t.operatorKeyword], color: palette.keyword },

			{ tag: [t.typeName, t.namespace, t.number, t.atom, t.bool], color: palette.typeName },
			{ tag: [t.function(t.variableName), t.labelName], color: accentColor },
			{
				tag: [t.className, t.definition(t.variableName), t.propertyName, t.attributeName],
				color: palette.className
			},

			{ tag: [t.variableName, t.name], color: palette.variableName },
			{ tag: [t.string, t.inserted, t.regexp, t.special(t.string)], color: palette.string },
			{ tag: [t.operator, t.url, t.link, t.escape], color: palette.operator },

			{ tag: [t.separator, t.punctuation], color: palette.separator },

			{ tag: t.heading, color: palette.variableName, fontWeight: 'bold' },
			{ tag: t.strong, fontWeight: 'bold' },
			{ tag: t.emphasis, fontStyle: 'italic' },
			{ tag: t.strikethrough, textDecoration: 'line-through' },
			{ tag: t.invalid, color: palette.invalid },
			{ tag: t.link, textDecoration: 'underline' }
		];

		return createTheme({
			theme,
			settings: { ...dynamicSettings, ...settings },
			styles: [...dynamicStyles, ...styles]
		});
	};
}

export const arcaneDarkInit = createArcaneThemeInit({
	theme: 'dark',
	background: '#0d1117',
	foreground: '#c9d1d9',
	gutterForeground: '#8b949e',
	comment: '#8b949e',
	keyword: '#ff7b72',
	typeName: '#ffa657',
	className: '#d2a8ff',
	variableName: '#e6edf3',
	string: '#7ee787',
	operator: '#a5d6ff',
	separator: '#6e7681',
	invalid: '#f85149'
});

export const arcaneLightInit = createArcaneThemeInit({
	theme: 'light',
	background: '#ffffff',
	foreground: '#24292f',
	gutterForeground: '#8c959f',
	comment: '#6e7781',
	keyword: '#cf222e',
	typeName: '#953800',
	className: '#8250df',
	variableName: '#24292f',
	string: '#0a3069',
	operator: '#0550ae',
	separator: '#6e7781',
	invalid: '#cf222e'
});
