import { writable } from 'svelte/store';
import { syncBrowserThemeColor } from '$lib/utils/application-theme-util';

export const oledModeStore = writable<boolean>(false);

const OLED_CLASS = 'oled';

/**
 * Applies or removes OLED mode by toggling the `.oled` class on <html>.
 * The CSS rule only takes effect for the default application theme while dark
 * mode is active, so calling this in other states is safe.
 */
export function applyOledMode(enabled: boolean): void {
	oledModeStore.set(enabled);

	if (typeof document === 'undefined') {
		return;
	}

	if (enabled) {
		document.documentElement.classList.add(OLED_CLASS);
	} else {
		document.documentElement.classList.remove(OLED_CLASS);
	}

	syncBrowserThemeColor();
}
