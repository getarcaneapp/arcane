import { MediaQuery } from 'svelte/reactivity';

class IsTouchDevice extends MediaQuery {
	constructor() {
		super('(hover: none)', false);
	}
}

export const isTouchDevice = new IsTouchDevice();
