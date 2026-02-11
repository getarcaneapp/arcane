import { settingsService } from '$lib/services/settings-service';
import type { Settings } from '$lib/types/settings.type';
import { applyAccentColor } from '$lib/utils/accent-color-util';
import { writable } from 'svelte/store';

const settingsStore = writable<Settings>();

const reload = async () => {
	const settings = await settingsService.getSettings();

	set(settings);
};

const set = (settings: Settings) => {
	applyAccentColor(settings.accentColor);
	settingsStore.set(settings);
};

export default {
	subscribe: settingsStore.subscribe,
	reload,
	set
};
