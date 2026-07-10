import type { TimeFormat } from '$lib/types/auth';

let currentTimeFormat = $state<TimeFormat>('auto');

export const timeFormatStore = {
	get current(): TimeFormat {
		return currentTimeFormat;
	},
	set(value: TimeFormat) {
		currentTimeFormat = value;
	},
	reset() {
		currentTimeFormat = 'auto';
	}
};
