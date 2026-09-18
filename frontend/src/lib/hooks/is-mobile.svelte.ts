import { MediaQuery } from 'svelte/reactivity';
import { getLayoutMode } from '#lib/stores/layout-mode.store.svelte.js';

const MOBILE_BREAKPOINT = 768;

export class IsMobile extends MediaQuery {
	#honorLayoutMode: boolean;

	/**
	 * @param honorLayoutMode When false, only the viewport width is consulted. Use
	 * this for decisions about physical space (e.g. editor heights), not layout.
	 */
	constructor({ honorLayoutMode = true }: { honorLayoutMode?: boolean } = {}) {
		super(`max-width: ${MOBILE_BREAKPOINT - 1}px`);
		this.#honorLayoutMode = honorLayoutMode;
	}

	override get current(): boolean {
		// Always read the media query first so its subscription stays registered
		// regardless of the current mode.
		const matches = super.current;
		if (!this.#honorLayoutMode) return matches;
		const mode = getLayoutMode();
		return mode === 'auto' ? matches : mode === 'mobile';
	}
}
