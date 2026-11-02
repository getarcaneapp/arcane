import type { ApplicationTheme, ThemeDefinition, ThemePalette } from '#lib/types/settings.js';
import { trySync } from '#lib/utils/try-catch.js';

type ThemePreview = Pick<ThemePalette, 'background' | 'sidebar' | 'card' | 'border' | 'foreground' | 'primary'>;

export type ApplicationThemeOption = Pick<ThemeDefinition, 'id' | 'name' | 'description'> & {
	light: ThemePreview;
	dark: ThemePreview;
};

const DEFAULT_THEME_OPTION: ApplicationThemeOption = {
	id: 'default',
	light: {
		background: '#fafafa',
		sidebar: '#f3f4f6',
		card: '#ffffff',
		border: '#d4d4d8',
		foreground: '#18181b',
		primary: '#8b5cf6'
	},
	dark: {
		background: '#24262b',
		sidebar: '#1c1f24',
		card: '#31343a',
		border: '#4a4f58',
		foreground: '#f5f7fa',
		primary: '#a855f7'
	}
};

const BUILT_IN_THEMES: readonly ThemeDefinition[] = Object.values(
	import.meta.glob<ThemeDefinition>('../themes/*.json', { eager: true, import: 'default' })
);

let customThemes = $state.raw<readonly ThemeDefinition[]>([]);

export function setCustomThemes(themes: readonly ThemeDefinition[]): void {
	customThemes = themes;
}

export function getApplicationThemeOptions(): ApplicationThemeOption[] {
	return [DEFAULT_THEME_OPTION, ...BUILT_IN_THEMES, ...customThemes];
}

export function findApplicationTheme(id: string): ThemeDefinition | undefined {
	return [...BUILT_IN_THEMES, ...customThemes].find((theme) => theme.id === id);
}

function buildThemeCss(theme: ThemeDefinition): string {
	const block = (selector: string, palette: ThemePalette) =>
		`${selector} { ${Object.entries({
			...palette,
			'bg-surface': 'var(--surface)',
			'glass-base': 'var(--surface)',
			'glass-tint': 'var(--primary)'
		})
			.map(([token, value]) => `--${token}: ${value};`)
			.join(' ')} }`;

	return [
		block(`:root[data-app-theme='${theme.id}']`, theme.light),
		block(`:root.dark[data-app-theme='${theme.id}']`, theme.dark)
	].join('\n');
}

const APP_THEME_ATTRIBUTE = 'data-app-theme';
const APP_THEME_STYLE_ID = 'arcane-app-theme';
const OLED_CLASS = 'oled';
const DARK_CLASS = 'dark';
const OLED_THEME_COLOR = '#000000';
const DEFAULT_ACCENT_COLOR = 'oklch(0.606 0.25 292.717)';

let htmlClassObserver: MutationObserver | null = null;

// Mirrors the appearance state onto localStorage so the blocking inline script
// in app.html can restore it before first paint (see app.html) — without this
// the SPA shell paints the default light theme until hydration + API calls finish.
const APPEARANCE_CACHE_KEY = 'arcane-appearance';

type AppearanceCache = {
	appTheme?: ApplicationTheme | null;
	themeCss?: string | null;
	oled?: boolean;
	accent?: { primary: string; primaryForeground: string; ring: string } | null;
	fontSize?: number | null;
	noBlur?: boolean;
	reduceEffects?: boolean;
};

function updateAppearanceCache(partial: AppearanceCache): void {
	if (typeof localStorage === 'undefined') {
		return;
	}

	const stored = trySync(() => JSON.parse(localStorage.getItem(APPEARANCE_CACHE_KEY) ?? '{}') as { v?: AppearanceCache });
	const v: AppearanceCache = { ...stored.data?.v, ...partial };

	// Pre-compute the exact attribute, class list, and inline style the inline
	// script applies, so it stays a dumb three-line replay of this cache.
	const attr = v.appTheme ?? '';
	const cls = [v.oled && OLED_CLASS, v.noBlur && 'no-blur', v.reduceEffects && 'reduce-effects'].filter(Boolean).join(' ');
	const css = Object.entries({
		'--primary': v.accent?.primary,
		'--primary-foreground': v.accent?.primaryForeground,
		'--ring': v.accent?.ring,
		'--sidebar-ring': v.accent?.ring,
		'font-size': v.fontSize && `${v.fontSize}px`
	})
		.filter(([, value]) => value)
		.map(([property, value]) => `${property}: ${value}`)
		.join('; ');

	trySync(() => localStorage.setItem(APPEARANCE_CACHE_KEY, JSON.stringify({ v, attr, cls, css })));
}

const appearanceState = $state({ theme: 'default' as ApplicationTheme, accent: 'default' });

export function resolveThemePrimary(isDarkMode: boolean): string {
	const theme = findApplicationTheme(appearanceState.theme);

	if (!theme) {
		return DEFAULT_ACCENT_COLOR;
	}

	return isDarkMode ? theme.dark.primary : theme.light.primary;
}

export function resolveLogoColor(isDarkMode: boolean): string {
	if (appearanceState.accent !== 'default' && appearanceState.accent !== 'theme') {
		return appearanceState.accent;
	}

	return resolveThemePrimary(isDarkMode);
}

export function resolveApplicationTheme(value?: string | null): ApplicationTheme {
	return value && findApplicationTheme(value) ? value : 'default';
}

export function applyApplicationTheme(themeValue?: string | null): void {
	const theme = resolveApplicationTheme(themeValue);
	appearanceState.theme = theme;

	if (typeof document === 'undefined') {
		return;
	}

	ensureThemeColorSyncObserver();

	const definition = findApplicationTheme(theme);
	const existingStyle = document.getElementById(APP_THEME_STYLE_ID);

	if (!definition) {
		existingStyle?.remove();
		document.documentElement.removeAttribute(APP_THEME_ATTRIBUTE);
		updateAppearanceCache({ appTheme: null, themeCss: null });
		syncBrowserThemeColor();
		return;
	}

	const themeCss = buildThemeCss(definition);
	const style =
		existingStyle ?? document.head.appendChild(Object.assign(document.createElement('style'), { id: APP_THEME_STYLE_ID }));
	style.textContent = themeCss;
	document.documentElement.setAttribute(APP_THEME_ATTRIBUTE, theme);
	updateAppearanceCache({ appTheme: theme, themeCss });

	syncBrowserThemeColor();
}

function isDefaultApplicationTheme(root: HTMLElement): boolean {
	const appTheme = root.getAttribute(APP_THEME_ATTRIBUTE);
	return !appTheme || appTheme === 'default';
}

function ensureThemeColorSyncObserver(): void {
	if (typeof document === 'undefined' || htmlClassObserver) {
		return;
	}

	htmlClassObserver = new MutationObserver(() => {
		syncBrowserThemeColor();
	});

	htmlClassObserver.observe(document.documentElement, {
		attributes: true,
		attributeFilter: ['class', APP_THEME_ATTRIBUTE]
	});
}

function syncBrowserThemeColor(): void {
	if (typeof document === 'undefined') {
		return;
	}

	const root = document.documentElement;
	const isDarkMode = root.classList.contains(DARK_CLASS);
	const activeMeta = document.querySelector<HTMLMetaElement>(
		`meta[name="theme-color"][media="(prefers-color-scheme: ${isDarkMode ? 'dark' : 'light'})"]`
	);

	if (!activeMeta) {
		return;
	}

	if (isDarkMode && root.classList.contains(OLED_CLASS) && isDefaultApplicationTheme(root)) {
		activeMeta.content = OLED_THEME_COLOR;
		return;
	}

	const background = getComputedStyle(root).getPropertyValue('--background').trim();

	if (background) {
		activeMeta.content = background;
	}
}

export function applyAccentColor(accentValue: string) {
	// The 'theme' sentinel means "inherit the application theme" and behaves
	// exactly like 'default' (no override). It is normalized here so invalid
	// CSS can never reach the custom-accent branch below.
	const normalizedAccent = accentValue === 'theme' ? 'default' : accentValue;
	const resolvedAccent = normalizedAccent === 'default' ? DEFAULT_ACCENT_COLOR : normalizedAccent;
	appearanceState.accent = normalizedAccent;

	if (typeof document === 'undefined') {
		return;
	}

	if (normalizedAccent === 'default') {
		document.documentElement.style.removeProperty('--primary');
		document.documentElement.style.removeProperty('--primary-foreground');
		document.documentElement.style.removeProperty('--ring');
		document.documentElement.style.removeProperty('--sidebar-ring');
		updateAppearanceCache({ accent: null });
		return;
	}

	document.documentElement.style.setProperty('--primary', resolvedAccent);

	const foregroundColor = getContrastingForeground(resolvedAccent);
	document.documentElement.style.setProperty('--primary-foreground', foregroundColor);

	const ringColor = `color-mix(in srgb, ${resolvedAccent} 50%, transparent)`;
	document.documentElement.style.setProperty('--ring', ringColor);
	document.documentElement.style.setProperty('--sidebar-ring', ringColor);

	updateAppearanceCache({ accent: { primary: resolvedAccent, primaryForeground: foregroundColor, ring: ringColor } });
}

function getContrastingForeground(color: string): string {
	const brightness = getColorBrightness(color);
	return brightness < 0.55 ? 'oklch(0.98 0 0)' : 'oklch(0.09 0 0)';
}

function getColorBrightness(color: string): number {
	const tempElement = document.createElement('div');
	tempElement.style.color = color;
	document.body.appendChild(tempElement);

	const computedColor = window.getComputedStyle(tempElement).color;
	document.body.removeChild(tempElement);

	const rgbMatch = computedColor.match(/rgb\((\d+),\s*(\d+),\s*(\d+)\)/);
	if (!rgbMatch) {
		return 0.5;
	}

	const r = Number.parseInt(rgbMatch[1] ?? '0', 10);
	const g = Number.parseInt(rgbMatch[2] ?? '0', 10);
	const b = Number.parseInt(rgbMatch[3] ?? '0', 10);

	const sR = r / 255;
	const sG = g / 255;
	const sB = b / 255;

	const rLinear = sR <= 0.03928 ? sR / 12.92 : Math.pow((sR + 0.055) / 1.055, 2.4);
	const gLinear = sG <= 0.03928 ? sG / 12.92 : Math.pow((sG + 0.055) / 1.055, 2.4);
	const bLinear = sB <= 0.03928 ? sB / 12.92 : Math.pow((sB + 0.055) / 1.055, 2.4);

	return 0.2126 * rLinear + 0.7152 * gLinear + 0.0722 * bLinear;
}

export function applyOledMode(enabled: boolean): void {
	if (typeof document === 'undefined') {
		return;
	}

	if (enabled) {
		document.documentElement.classList.add(OLED_CLASS);
	} else {
		document.documentElement.classList.remove(OLED_CLASS);
	}

	updateAppearanceCache({ oled: enabled });

	syncBrowserThemeColor();
}

const NO_BLUR_CLASS = 'no-blur';
const REDUCE_EFFECTS_CLASS = 'reduce-effects';

/**
 * Toggle glass / backdrop-blur effects. When disabled, the `no-blur` class on
 * <html> drives layout.css to strip every backdrop-filter and make glass surfaces
 * opaque — the main lever for cutting GPU usage on Firefox-based browsers.
 */
export function applyGlassEffects(enabled: boolean): void {
	if (typeof document === 'undefined') {
		return;
	}

	document.documentElement.classList.toggle(NO_BLUR_CLASS, !enabled);
	updateAppearanceCache({ noBlur: !enabled });
}

/**
 * Toggle decorative, always-on interface animations (ambient login shimmer,
 * loading-bar glow, pulse indicators). When disabled we add a `reduce-effects`
 * class to <html> and layout.css halts those animations.
 */
export function applyInterfaceAnimations(enabled: boolean): void {
	if (typeof document === 'undefined') {
		return;
	}

	document.documentElement.classList.toggle(REDUCE_EFFECTS_CLASS, !enabled);
	updateAppearanceCache({ reduceEffects: !enabled });
}

export const FONT_SIZE_MIN = 12;
export const FONT_SIZE_MAX = 20;
export const FONT_SIZE_DEFAULT = 14; // matches `html { font-size: 14px }` in layout.css

/**
 * Apply the user's preferred root font size. The UI is sized in `rem` off the
 * root element, so this scales text and spacing together (a whole-UI zoom). At
 * the default size we remove the inline override so the CSS rule wins.
 */
export function applyFontSize(px?: number | null): void {
	if (typeof document === 'undefined') {
		return;
	}

	const root = document.documentElement;

	if (px == null || px === FONT_SIZE_DEFAULT) {
		root.style.removeProperty('font-size');
		updateAppearanceCache({ fontSize: null });
		return;
	}

	const clamped = Math.min(FONT_SIZE_MAX, Math.max(FONT_SIZE_MIN, Math.round(px)));
	root.style.fontSize = `${clamped}px`;
	updateAppearanceCache({ fontSize: clamped });
}
