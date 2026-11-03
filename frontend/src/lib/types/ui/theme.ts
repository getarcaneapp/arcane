import type { CustomTheme, ThemePalette } from '../user';

export const THEME_TOKENS = [
	'background',
	'foreground',
	'card',
	'card-foreground',
	'popover',
	'popover-foreground',
	'primary',
	'primary-foreground',
	'secondary',
	'secondary-foreground',
	'muted',
	'muted-foreground',
	'accent',
	'accent-foreground',
	'border',
	'input',
	'ring',
	'sidebar',
	'sidebar-foreground',
	'sidebar-primary',
	'sidebar-primary-foreground',
	'sidebar-accent',
	'sidebar-accent-foreground',
	'sidebar-border',
	'sidebar-ring',
	'surface',
	'glass-shadow-color',
	'syntax-comment',
	'syntax-keyword',
	'syntax-property',
	'syntax-string',
	'syntax-number',
	'syntax-operator'
] as const satisfies readonly (keyof ThemePalette)[];

export type ThemeToken = (typeof THEME_TOKENS)[number];

export type ThemeDefinition = Omit<CustomTheme, 'name'> & {
	name?: string;
};
