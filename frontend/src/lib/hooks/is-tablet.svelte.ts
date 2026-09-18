import { MediaQuery } from 'svelte/reactivity';
import { getLayoutMode } from '#lib/stores/layout-mode.store.svelte.js';

// Breakpoint for tablet/small desktop where sidebar should auto-collapse
const TABLET_BREAKPOINT = 1024;

export class IsTablet extends MediaQuery {
	constructor() {
		super(`max-width: ${TABLET_BREAKPOINT - 1}px`);
	}

	override get current(): boolean {
		// Always read the media query first so its subscription stays registered.
		const matches = super.current;
		// A forced desktop layout must behave like a desktop: the tablet lock would
		// otherwise keep the sidebar collapsed with no way to expand or pin it.
		return getLayoutMode() === 'desktop' ? false : matches;
	}
}
