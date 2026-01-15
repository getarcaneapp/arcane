import { browser } from '$app/environment';
import { environmentManagementService } from '$lib/services/env-mgmt-service';
import { settingsService } from '$lib/services/settings-service';
import { userService } from '$lib/services/user-service';
import versionService from '$lib/services/version-service';
import settingsStore from '$lib/stores/config-store';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import userStore from '$lib/stores/user-store';
import { type AppVersionInformation } from '$lib/types/application-configuration';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { authService } from '$lib/services/auth-service';
import { tryCatch } from '$lib/utils/try-catch';
import { QueryClient } from '@tanstack/svelte-query';
import { queryKeys } from '$lib/query/query-keys';
import { systemService } from '$lib/services/system-service';
import type { DockerInfo } from '$lib/types/docker-info.type';

export const ssr = false;

export const load = async () => {
	// Tanstack Query Client
	const queryClient = new QueryClient({
		defaultOptions: {
			queries: {
				enabled: browser,
				staleTime: 0,
				gcTime: 60 * 1000,
				refetchOnMount: 'always',
				refetchOnWindowFocus: 'always',
				refetchOnReconnect: 'always'
			}
		}
	});

	// Step 1: Check authentication first
	let user = await userService.getCurrentUser().catch(() => null);

	// Step 1.5: Check auto-login config (and attempt auto-login if enabled)
	if (browser) {
		const autoLoginConfig = await queryClient.fetchQuery({
			queryKey: queryKeys.auth.autoLoginConfig(),
			queryFn: () => authService.getAutoLoginConfig()
		});

		if (autoLoginConfig) {
			if (autoLoginConfig.enabled) {
				settingsStore.autoLoginEnabled.set(true);
				settingsStore.autoLoginEnabled.clearDisabledCache();
				if (!user) {
					// Attempt auto-login using server-configured credentials
					user = await queryClient.fetchQuery({
						queryKey: queryKeys.auth.autoLoginAttempt(),
						queryFn: () => authService.attemptAutoLogin()
					});
				}
			} else {
				settingsStore.autoLoginEnabled.set(false);
				// Cache that auto-login is disabled to avoid checking on every page load
				settingsStore.autoLoginEnabled.cacheDisabled();
			}
		}
	}

	// Step 2: Only fetch authenticated data if user is logged in
	let settings = null;
	let dockerInfo: DockerInfo | null = null;

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
		dockerInfo = await systemService.getDockerInfo().catch(() => null);
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
		enabledFeatures: [],
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
			enabledFeatures: info.enabledFeatures ?? [],
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
		queryClient,
		dockerInfo
	};
};
