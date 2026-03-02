import { writable } from 'svelte/store';

export const oledModeStore = writable<boolean>(false);

const OLED_CLASS = 'oled';
const OLED_THEME_COLOR = '#000000';
const DARK_THEME_COLOR = 'oklch(0.141 0.005 285.823)';
let currentOledEnabled = false;
let htmlClassObserver: MutationObserver | null = null;

function ensureThemeColorSyncObserver(): void {
	if (typeof document === 'undefined' || htmlClassObserver) {
		return;
	}

	htmlClassObserver = new MutationObserver(() => {
		updateThemeColorMeta(currentOledEnabled);
	});

	htmlClassObserver.observe(document.documentElement, {
		attributes: true,
		attributeFilter: ['class']
	});
}

/**
 * Applies or removes OLED mode by toggling the `.oled` class on <html>.
 * The CSS rule `.dark.oled` only takes effect when dark mode is also active,
 * so calling this while in light mode is safe — it adds the class but the
 * CSS selector will not match until dark mode is enabled.
 *
 * Also updates the `theme-color` meta tag when in dark mode to #000000.
 */
export function applyOledMode(enabled: boolean): void {
	currentOledEnabled = enabled;
	oledModeStore.set(enabled);

	if (typeof document === 'undefined') {
		return;
	}

	ensureThemeColorSyncObserver();

	if (enabled) {
		document.documentElement.classList.add(OLED_CLASS);
	} else {
		document.documentElement.classList.remove(OLED_CLASS);
	}

	updateThemeColorMeta(enabled);
}

/**
 * Updates the theme-color meta tag for the dark scheme.
 * When OLED is active we want #000000 so the browser chrome (e.g. Safari iOS)
 * also goes fully black.
 */
function updateThemeColorMeta(oledEnabled: boolean): void {
	if (typeof document === 'undefined') return;

	const isDark = document.documentElement.classList.contains('dark');
	const darkMeta = document.querySelector<HTMLMetaElement>('meta[name="theme-color"][media="(prefers-color-scheme: dark)"]');
	if (darkMeta) {
		darkMeta.content = oledEnabled && isDark ? OLED_THEME_COLOR : DARK_THEME_COLOR;
	}
}
