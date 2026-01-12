import { environmentStore } from '$lib/stores/environment.store.svelte';
import versionService from '$lib/services/version-service';
import { tryCatch } from '$lib/utils/try-catch';
import userStore from '$lib/stores/user-store';
import settingsStore from '$lib/stores/config-store';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import type { AppVersionInformation } from '$lib/types/application-configuration';
import { userService } from '$lib/services/user-service';
import { settingsService } from '$lib/services/settings-service';
import { environmentManagementService } from '$lib/services/env-mgmt-service';
import { authService } from '$lib/services/auth-service';
import { browser } from '$app/environment';

export const ssr = false;

// Session storage key to cache when auto-login is disabled on the backend
const AUTO_LOGIN_DISABLED_KEY = 'arcane_auto_login_disabled';

/**
 * Check if auto-login is known to be disabled (cached from previous check).
 */
function isAutoLoginKnownDisabled(): boolean {
	if (!browser) return true;
	try {
		return sessionStorage.getItem(AUTO_LOGIN_DISABLED_KEY) === 'true';
	} catch {
		return false;
	}
}

/**
 * Cache that auto-login is disabled to avoid unnecessary API calls.
 */
function cacheAutoLoginDisabled(): void {
	if (!browser) return;
	try {
		sessionStorage.setItem(AUTO_LOGIN_DISABLED_KEY, 'true');
	} catch {
		// Ignore storage errors
	}
}

export const load = async () => {
	// Step 1: Check authentication first
	let user = await userService.getCurrentUser().catch(() => null);
	let autoLoginEnabled = false;

	// Step 1.5: Attempt auto-login if not authenticated
	if (!user && browser && !isAutoLoginKnownDisabled()) {
		// Check if auto-login is enabled
		const autoLoginConfig = await authService.getAutoLoginConfig();

		if (autoLoginConfig?.enabled) {
			autoLoginEnabled = true;
			// Attempt auto-login using server-configured credentials
			user = await authService.attemptAutoLogin();
		} else {
			// Cache that auto-login is disabled to avoid checking on every page load
			cacheAutoLoginDisabled();
		}
	} else if (user && browser && !isAutoLoginKnownDisabled()) {
		// User is already logged in, check if auto-login is enabled (for password change dialog skip)
		const autoLoginConfig = await authService.getAutoLoginConfig().catch(() => null);
		if (autoLoginConfig?.enabled) {
			autoLoginEnabled = true;
		} else {
			cacheAutoLoginDisabled();
		}
	}

	// Step 2: Only fetch authenticated data if user is logged in
	let settings = null;

	if (user) {
		// Initialize environment store (required for settings service)
		const environmentRequestOptions: SearchPaginationSortRequest = {
			pagination: {
				page: 1,
				limit: 1000
			}
		};

		const environments = await tryCatch(environmentManagementService.getEnvironments(environmentRequestOptions));
		if (!environments.error) {
			await environmentStore.initialize(environments.data.data);
		} else {
			await environmentStore.initialize([]);
		}

		// Fetch settings after environment store is initialized
		// Settings service depends on environmentStore.getCurrentEnvironmentId()
		settings = await settingsService.getSettings().catch(() => null);
	} else {
		// Initialize empty environment store for unauthenticated users
		await environmentStore.initialize([]);

		// Try to fetch public settings for login page configuration
		settings = await settingsService.getPublicSettings().catch(() => null);
	}

	// Step 3: Update stores with fetched data (always, even if null)
	if (user) {
		await userStore.setUser(user);
	}

	if (settings) {
		settingsStore.set(settings);
	}

	// Step 4: Fetch version information (independent, works for all users)
	let versionInformation: AppVersionInformation = {
		currentVersion: versionService.getCurrentVersion(),
		displayVersion: versionService.getCurrentVersion(),
		revision: 'unknown',
		shortRevision: 'unknown',
		goVersion: 'unknown',
		isSemverVersion: false
	};

	try {
		const info = await versionService.getVersionInformation();
		versionInformation = {
			currentVersion: info.currentVersion,
			currentTag: info.currentTag,
			currentDigest: info.currentDigest,
			displayVersion: info.displayVersion,
			revision: info.revision,
			shortRevision: info.shortRevision || (info.revision?.slice(0, 8) ?? 'unknown'),
			goVersion: info.goVersion || 'unknown',
			buildTime: info.buildTime,
			isSemverVersion: info.isSemverVersion,
			newestVersion: info.newestVersion,
			newestDigest: info.newestDigest,
			updateAvailable: info.updateAvailable,
			releaseUrl: info.releaseUrl
		};
	} catch {}

	return {
		user,
		settings,
		versionInformation,
		autoLoginEnabled
	};
};
