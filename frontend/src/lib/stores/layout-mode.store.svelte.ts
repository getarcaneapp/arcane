import { PersistedState } from 'runed';

/**
 * Per-device layout override. Browsers' "Desktop site" toggles only change the
 * user agent on Chromium tablets, leaving the layout viewport (and therefore
 * every width media query) untouched, so users need an explicit switch. It is
 * device-local on purpose: the same account may want desktop on a tablet and
 * automatic on a phone.
 */
export type LayoutMode = 'auto' | 'mobile' | 'desktop';

export const LAYOUT_MODE_STORAGE_KEY = 'arcane-layout-mode';

export const layoutModeStore = new PersistedState<LayoutMode>(LAYOUT_MODE_STORAGE_KEY, 'auto');

/** Read the stored mode, treating anything unexpected in localStorage as `auto`. */
export function getLayoutMode(): LayoutMode {
	const value = layoutModeStore.current;
	return value === 'mobile' || value === 'desktop' ? value : 'auto';
}
