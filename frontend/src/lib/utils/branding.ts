import settingsStore from '#lib/stores/config-store.svelte.js';
import { versionStore } from '#lib/stores/version.store.svelte.js';

// Mirrors the backend's stable check: plain X.Y.Z, optionally with a v prefix or build metadata.
export function isDevelopmentBuild(): boolean {
	const version = versionStore.current?.currentVersion.trim();
	return !!version && !/^v?\d+\.\d+\.\d+(\+.*)?$/.test(version);
}

// Development builds serve a square tile mark, which needs a larger box than the stable glyph.
export function usesDevelopmentBranding(): boolean {
	return isDevelopmentBuild() && settingsStore.current?.developmentBrandingEnabled !== false;
}
