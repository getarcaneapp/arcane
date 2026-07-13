import { browser } from '$app/env';
import { environmentManagementService } from '$lib/services/env-mgmt-service';
import { settingsService } from '$lib/services/settings-service';
import { roleService } from '$lib/services/role-service';
import { swarmService } from '$lib/services/swarm-service';
import { userService } from '$lib/services/user-service';
import versionService from '$lib/services/version-service';
import settingsStore from '$lib/stores/config-store';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import userStore from '$lib/stores/user-store';
import { type AppVersionInformation } from '$lib/types/settings';
import type { SearchPaginationSortRequest } from '$lib/types/shared';
import type { PermissionsManifest } from '$lib/types/auth';
import { authService } from '$lib/services/auth-service';
import { tryCatch } from '$lib/utils/api';
import { QueryClient } from '@tanstack/svelte-query';
import { queryKeys } from '$lib/query/query-keys';

export const ssr = false;

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

let authenticatedUserId: string | null | undefined;

export const load = async () => {
	const versionInformationRequest = versionService.getVersionInformation();
	const autoLoginConfigRequest = browser
		? queryClient.fetchQuery({
				queryKey: queryKeys.auth.autoLoginConfig(),
				queryFn: () => authService.getAutoLoginConfig()
			})
		: Promise.resolve(null);
	let [user, autoLoginConfig] = await Promise.all([userService.getCurrentUser().catch(() => null), autoLoginConfigRequest]);

	if (autoLoginConfig) {
		if (autoLoginConfig.enabled) {
			settingsStore.autoLoginEnabled.set(true);
			settingsStore.autoLoginEnabled.clearDisabledCache();
			if (!user) {
				user = await queryClient.fetchQuery({
					queryKey: queryKeys.auth.autoLoginAttempt(),
					queryFn: () => authService.attemptAutoLogin()
				});
			}
		} else {
			settingsStore.autoLoginEnabled.set(false);
			settingsStore.autoLoginEnabled.cacheDisabled();
		}
	}

	const nextAuthenticatedUserId = user?.id ?? null;
	if (authenticatedUserId !== undefined && authenticatedUserId !== nextAuthenticatedUserId) {
		authService.resetAuthenticatedState(queryClient, { restartMountedStores: user !== null });
	}
	authenticatedUserId = nextAuthenticatedUserId;

	let settings = null;
	let swarmEnabled = false;
	let permissionsManifest: PermissionsManifest | null = null;
	let permissionsManifestLoadFailed = false;
	if (user) {
		// Initialize environment store (required for settings service)
		const environmentRequestOptions: SearchPaginationSortRequest = {
			pagination: {
				page: 1,
				limit: 1000
			}
		};

		const environmentsRequest = tryCatch(environmentManagementService.getEnvironments(environmentRequestOptions));
		const permissionsManifestRequest = roleService.getPermissionsManifest().catch((): PermissionsManifest | null => null);
		const environments = await environmentsRequest;
		if (!environments.error) {
			await environmentStore.initialize(environments.data.data);
		} else {
			await environmentStore.initialize([]);
		}

		const [loadedSettings, loadedSwarmStatus, loadedPermissionsManifest] = await Promise.all([
			settingsService.getSettings().catch(() => null),
			swarmService.getSwarmStatus().catch(() => null),
			permissionsManifestRequest
		]);
		settings = loadedSettings;
		swarmEnabled = loadedSwarmStatus?.enabled === true;
		permissionsManifest = loadedPermissionsManifest;
		permissionsManifestLoadFailed = loadedPermissionsManifest === null;
	} else {
		// Initialize empty environment store for unauthenticated users
		await environmentStore.initialize([]);

		// Try to fetch public settings for login page configuration
		settings = await settingsService.getPublicSettings().catch(() => null);
	}

	if (user) {
		await userStore.setUser(user);
	} else {
		userStore.clearUser();
	}

	if (settings) {
		settingsStore.set(settings);
	}

	let versionInformation: AppVersionInformation = {
		currentVersion: versionService.getCurrentVersion(),
		displayVersion: versionService.getCurrentVersion(),
		revision: 'unknown',
		shortRevision: 'unknown',
		goVersion: 'unknown',
		nodeVersion: 'unknown',
		svelteKitVersion: 'unknown',
		enabledFeatures: [],
		isSemverVersion: false
	};

	try {
		const info = await versionInformationRequest;
		versionInformation = {
			currentVersion: info.currentVersion,
			currentTag: info.currentTag,
			currentDigest: info.currentDigest,
			displayVersion: info.displayVersion,
			revision: info.revision,
			shortRevision: info.shortRevision || (info.revision?.slice(0, 8) ?? 'unknown'),
			goVersion: info.goVersion || 'unknown',
			nodeVersion: info.nodeVersion || 'unknown',
			svelteKitVersion: info.svelteKitVersion || 'unknown',
			enabledFeatures: info.enabledFeatures ?? [],
			buildTime: info.buildTime,
			isSemverVersion: info.isSemverVersion,
			newestVersion: info.newestVersion,
			newestDigest: info.newestDigest,
			updateAvailable: info.updateAvailable,
			releaseUrl: info.releaseUrl,
			releaseNotes: info.releaseNotes,
			releasedAt: info.releasedAt
		};
	} catch {}

	return {
		user,
		settings,
		permissionsManifest,
		permissionsManifestLoadFailed,
		versionInformation,
		queryClient,
		swarmEnabled
	};
};
