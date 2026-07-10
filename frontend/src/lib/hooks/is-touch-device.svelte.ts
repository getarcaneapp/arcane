import { MediaQuery } from 'svelte/reactivity';

/**
 * Detects when the primary input cannot hover and should use touch-first UI.
 */
export class IsTouchDevice extends MediaQuery {
	constructor() {
		super('(hover: none)', false);
	}
}
