import settingsStore from '#lib/stores/config-store.svelte.js';
import { versionStore } from '#lib/stores/version.store.svelte.js';

export function isDevelopmentBuild(): boolean {
	const version = versionStore.current;
	return !!version?.currentVersion.trim() && !version.isStableVersion;
}

// Development builds serve a square tile mark, which needs a larger box than the stable glyph.
export function usesDevelopmentBranding(): boolean {
	return isDevelopmentBuild() && settingsStore.current?.developmentBrandingEnabled !== false;
}

export function logoBranding(full = false): { custom: string; development: boolean } {
	const custom = (full ? settingsStore.current?.customLogoFullHash : settingsStore.current?.customLogoHash) ?? '';
	return { custom, development: !custom && usesDevelopmentBranding() };
}
