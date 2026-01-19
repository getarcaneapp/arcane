import { settingsService } from '$lib/services/settings-service';
import type { Settings } from '$lib/types/settings.type';
import { applyAccentColor } from '$lib/utils/accent-color-util';
import { bustLogoCache } from '$lib/utils/image.util';
import { get, writable } from 'svelte/store';

const settingsStore = writable<Settings>();

const reload = async () => {
	const previousSettings = get(settingsStore);
	const settings = await settingsService.getSettings();

	// Bust logo cache if accent color changed
	if (previousSettings && previousSettings.accentColor !== settings.accentColor) {
		bustLogoCache();
	}

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
