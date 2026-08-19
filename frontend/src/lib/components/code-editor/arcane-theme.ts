import { registerCustomTheme, type ThemeRegistration } from '@pierre/diffs';

// Arcane syntax themes: Pierre Dark/Light editor chrome (background, gutters,
// selection, diff colors) with token colors carried over from the pre-Pierre
// Arcane editor palette, plus the live accent color for function names.
export const ARCANE_EDITOR_THEMES = { dark: 'arcane-dark', light: 'arcane-light' } as const;

type TokenPalette = {
	comment: string;
	keyword: string;
	constant: string;
	className: string;
	variableName: string;
	string: string;
	operator: string;
	separator: string;
	invalid: string;
};

// Violet-centric palette keyed to Arcane's primary accent: keys/keywords in
// violet, values in sky blue, constants in amber — no red/green christmas tree.
const DARK_TOKENS: TokenPalette = {
	comment: '#71717a',
	keyword: '#a78bfa',
	constant: '#fbbf24',
	className: '#c4b5fd',
	variableName: '#e4e4e7',
	string: '#7dd3fc',
	operator: '#93c5fd',
	separator: '#a1a1aa',
	invalid: '#f87171'
};

const LIGHT_TOKENS: TokenPalette = {
	comment: '#71717a',
	keyword: '#7c3aed',
	constant: '#b45309',
	className: '#6d28d9',
	variableName: '#27272a',
	string: '#0369a1',
	operator: '#1d4ed8',
	separator: '#52525b',
	invalid: '#dc2626'
};

function accentColor(): string {
	const primary = getComputedStyle(document.documentElement).getPropertyValue('--primary').trim();
	return primary || 'oklch(0.606 0.25 292.717)';
}

function buildTokenColors(palette: TokenPalette) {
	return [
		{ scope: ['comment', 'punctuation.definition.comment'], settings: { foreground: palette.comment } },
		{ scope: ['keyword', 'storage', 'storage.type', 'storage.modifier'], settings: { foreground: palette.keyword } },
		// YAML/markup keys and property names.
		{ scope: ['entity.name.tag', 'support.type.property-name'], settings: { foreground: palette.keyword } },
		{ scope: ['support.type.property-name.json'], settings: { foreground: palette.className } },
		{
			scope: [
				'constant.numeric',
				'constant.language',
				'constant.other',
				'entity.name.type',
				'entity.name.class',
				'support.class'
			],
			settings: { foreground: palette.constant }
		},
		{ scope: ['entity.name.function', 'support.function'], settings: { foreground: accentColor() } },
		{
			scope: ['entity.other.attribute-name', 'meta.object-literal.key', 'variable.other.property'],
			settings: { foreground: palette.className }
		},
		{ scope: ['variable', 'variable.other', 'variable.parameter'], settings: { foreground: palette.variableName } },
		{ scope: ['string', 'string.unquoted', 'markup.inserted'], settings: { foreground: palette.string } },
		{
			scope: ['keyword.operator', 'string.regexp', 'constant.character.escape', 'markup.underline.link'],
			settings: { foreground: palette.operator }
		},
		{ scope: ['punctuation', 'meta.separator'], settings: { foreground: palette.separator } },
		{ scope: ['markup.heading'], settings: { foreground: palette.variableName, fontStyle: 'bold' } },
		{ scope: ['markup.bold'], settings: { fontStyle: 'bold' } },
		{ scope: ['markup.italic'], settings: { fontStyle: 'italic' } },
		{ scope: ['invalid', 'invalid.illegal'], settings: { foreground: palette.invalid } }
	];
}

let registered = false;

export function registerArcaneEditorThemes() {
	if (registered) return;
	registered = true;

	registerCustomTheme(ARCANE_EDITOR_THEMES.dark, async () => {
		const base = (await import('@pierre/theme/pierre-dark')).default;
		return {
			...base,
			name: ARCANE_EDITOR_THEMES.dark,
			displayName: 'Arcane Dark',
			tokenColors: buildTokenColors(DARK_TOKENS)
		} as ThemeRegistration;
	});

	registerCustomTheme(ARCANE_EDITOR_THEMES.light, async () => {
		const base = (await import('@pierre/theme/pierre-light')).default;
		return {
			...base,
			name: ARCANE_EDITOR_THEMES.light,
			displayName: 'Arcane Light',
			tokenColors: buildTokenColors(LIGHT_TOKENS)
		} as ThemeRegistration;
	});
}
