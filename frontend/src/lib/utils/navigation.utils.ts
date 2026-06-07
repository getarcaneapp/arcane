import { PersistedState } from 'runed';
import { defaultMobileNavigationSettings, type MobileNavigationSettings } from '$lib/config/navigation-config';
import settingsStore from '$lib/stores/config-store';
import { get } from 'svelte/store';
import type { MobileNavInteractionManager } from '$lib/hooks/use-mobile-nav-interactions';

export const pinnedItemsStore = new PersistedState('mobile-nav-settings', defaultMobileNavigationSettings);

export const navigationSettingsOverridesStore = new PersistedState<Partial<MobileNavigationSettings>>(
	'navigation-settings-overrides',
	{}
);

let mobileNavManager: MobileNavInteractionManager | null = null;

export function registerNavigationManager(manager: MobileNavInteractionManager) {
	mobileNavManager = manager;
}

export function resetNavigationVisibility() {
	if (mobileNavManager) {
		mobileNavManager.resetVisibility();
	}
}

export function getEffectiveNavigationSettings(): MobileNavigationSettings {
	const serverSettings = get(settingsStore);
	const overrides = navigationSettingsOverridesStore.current;
	const currentPinnedItems = pinnedItemsStore.current;

	// Helper function to get effective value (override > server > default)
	const getEffectiveValue = <T>(serverValue: T | undefined, overrideValue: T | undefined, defaultValue: T): T => {
		return overrideValue !== undefined ? overrideValue : (serverValue ?? defaultValue);
	};

	const mode = getEffectiveValue(serverSettings?.mobileNavigationMode, overrides.mode, defaultMobileNavigationSettings.mode);

	return {
		pinnedItems: overrides.pinnedItems ?? currentPinnedItems.pinnedItems,
		mode,
		showLabels: getEffectiveValue(
			serverSettings?.mobileNavigationShowLabels,
			overrides.showLabels,
			defaultMobileNavigationSettings.showLabels
		),
		// scrollToHide is now computed based on mode: always true for floating, always false for docked
		scrollToHide: mode === 'floating'
	};
}

export function updateNavigationOverrides(overrides: Partial<MobileNavigationSettings>) {
	const currentOverrides = navigationSettingsOverridesStore.current;
	navigationSettingsOverridesStore.current = { ...currentOverrides, ...overrides };
}

export function clearNavigationOverrides() {
	navigationSettingsOverridesStore.current = {};
}
