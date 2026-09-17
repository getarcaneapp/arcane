import { browser } from '$app/env';
import { environmentManagementService } from '#lib/services/env-mgmt-service.js';
import { settingsService } from '#lib/services/settings-service.js';
import { roleService } from '#lib/services/role-service.js';
import { swarmService } from '#lib/services/swarm-service.js';
import { userService } from '#lib/services/user-service.js';
import versionService from '#lib/services/version-service.js';
import settingsStore from '#lib/stores/config-store.svelte.js';
import { featureStore } from '#lib/stores/features.store.svelte.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import userStore from '#lib/stores/user-store.svelte.js';
import { versionStore } from '#lib/stores/version.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import type { PermissionsManifest, User } from '#lib/types/auth.js';
import { authService } from '#lib/services/auth-service.js';
import { tryCatch } from '#lib/utils/try-catch.js';
import { QueryClient } from '@tanstack/svelte-query';
import { queryKeys } from '#lib/query/query-keys.js';
import { redirect } from '@sveltejs/kit';
import { getAuthRedirectPath, userHasPermission } from '#lib/utils/auth.js';
import { isAuthRejectionError } from '#lib/utils/api.js';
import { getEffectiveLandingPage } from '#lib/utils/navigation.js';
import type { LayoutLoad } from './$types';

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

// The layout load re-runs on every navigation and re-checks the session. A
// transient failure of that check (network blip, 5xx, 429) must not sign the
// user out of the SPA: that clears every cache, bounces through /login and
// lands on the landing page. Only an explicit rejection means the session is
// gone; anything else keeps the user we already know about.
function resolveUserAfterLoadFailureInternal(error: unknown): User | null {
	if (isAuthRejectionError(error)) return null;
	return userStore.current;
}

export const load: LayoutLoad = async ({ url }) => {
	const versionInformationRequest = versionService.getVersionInformation();
	const autoLoginConfigRequest = browser
		? queryClient.query({
				queryKey: queryKeys.auth.autoLoginConfig(),
				queryFn: () => authService.getAutoLoginConfig()
			})
		: Promise.resolve(null);
	let [user, autoLoginConfig] = await Promise.all([
		tryCatch(userService.getCurrentUser()).then((result) =>
			result.error ? resolveUserAfterLoadFailureInternal(result.error) : result.data
		),
		autoLoginConfigRequest
	]);

	if (autoLoginConfig) {
		if (autoLoginConfig.enabled) {
			settingsStore.autoLoginEnabled.set(true);
			settingsStore.autoLoginEnabled.clearDisabledCache();
			if (!user) {
				user = await queryClient.query({
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
	if (user) {
		await userStore.setUser(user);
	} else {
		userStore.clearUser();
	}

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
		const permissionsManifestRequest = tryCatch(roleService.getPermissionsManifest()).then((result) =>
			result.error ? null : result.data
		);
		const environments = await environmentsRequest;
		if (!environments.error) {
			await environmentStore.initialize(environments.data.data);
		} else if (!environmentStore.isInitialized()) {
			// A failed refetch on an already-initialised store keeps the current
			// selection; re-initialising with [] would drop it to "No Environment".
			await environmentStore.initialize([]);
		}

		const settingsRequest = userHasPermission(user, 'settings:read')
			? tryCatch(settingsService.getSettings()).then(async (result) => {
					if (!result.error) return result.data;
					const publicSettings = await tryCatch(settingsService.getPublicSettings(environmentStore.selected?.id ?? '0'));
					return publicSettings.error ? null : publicSettings.data;
				})
			: tryCatch(settingsService.getPublicSettings(environmentStore.selected?.id ?? '0')).then((result) =>
					result.error ? null : result.data
				);
		featureStore.connect(queryClient);
		const featuresRequest = featureStore.refresh(await environmentStore.getCurrentEnvironmentId());
		const [loadedSettings, loadedSwarmStatus, loadedPermissionsManifest] = await Promise.all([
			settingsRequest,
			tryCatch(swarmService.getSwarmStatus()).then((result) => (result.error ? null : result.data)),
			permissionsManifestRequest
		]);
		// Keep recovery/settings pages reachable while a selected agent is offline.
		settings =
			loadedSettings ??
			(await tryCatch(settingsService.getPublicSettings()).then((result) => (result.error ? null : result.data)));
		await featuresRequest;
		swarmEnabled = loadedSwarmStatus?.enabled === true;
		permissionsManifest = loadedPermissionsManifest;
		permissionsManifestLoadFailed = loadedPermissionsManifest === null;
	} else {
		// Initialize empty environment store for unauthenticated users
		await environmentStore.initialize([]);

		// Try to fetch public settings for login page configuration
		settings = await tryCatch(settingsService.getPublicSettings()).then((result) => (result.error ? null : result.data));
	}

	if (settings) {
		settingsStore.set(settings);
	}

	const versionInformation = await versionInformationRequest;
	versionStore.seed(versionInformation);

	const redirectPath = getAuthRedirectPath(
		url.pathname,
		user,
		environmentStore.selected?.id || '0',
		permissionsManifest,
		permissionsManifestLoadFailed,
		getEffectiveLandingPage()
	);
	if (redirectPath && redirectPath !== url.pathname) {
		throw redirect(302, redirectPath);
	}

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
