import { browser } from '$app/env';
import { redirect } from '@sveltejs/kit';
import { QueryClient } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { isSessionCancelledError } from '#lib/services/api-service.js';
import { authService } from '#lib/services/auth-service.js';
import { environmentManagementService } from '#lib/services/env-mgmt-service.js';
import { roleService } from '#lib/services/role-service.js';
import { settingsService } from '#lib/services/settings-service.js';
import { swarmService } from '#lib/services/swarm-service.js';
import { userService } from '#lib/services/user-service.js';
import versionService, { toAppVersionInformation } from '#lib/services/version-service.js';
import settingsStore from '#lib/stores/config-store.svelte.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import { featureStore } from '#lib/stores/features.store.svelte.js';
import userStore from '#lib/stores/user-store.svelte.js';
import { versionStore } from '#lib/stores/version.store.svelte.js';
import type { PermissionsManifest, User } from '#lib/types/auth.js';
import { isAuthRejectionError } from '#lib/utils/api.js';
import { getAuthRedirectPath, userHasPermission } from '#lib/utils/auth.js';
import { getEffectiveLandingPage } from '#lib/utils/navigation.js';
import { setTelemetry } from '#lib/utils/telemetry.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { LayoutLoad } from './$types';

export const ssr = false;

const queryClient = new QueryClient({
	defaultOptions: {
		queries: {
			enabled: browser,
			// Covers the gap between a page loader's fetch and the component's
			// mount so the same query isn't fetched twice in one navigation.
			staleTime: 2_000,
			gcTime: 60 * 1000,
			refetchOnMount: true,
			refetchOnWindowFocus: 'always',
			refetchOnReconnect: 'always'
		}
	}
});

let authenticatedUserId: string | null | undefined;

function dataOrNull<T>(promise: Promise<T>): Promise<T | null> {
	return tryCatch(promise).then((result) => (result.error ? null : result.data));
}

export const load: LayoutLoad = async ({ url }) => {
	// Logout must not wait on, or be triggered by, authenticated requests; preloading runs this too.
	if (url.pathname === '/logout') {
		return {
			user: null,
			settings: settingsStore.current ?? null,
			permissionsManifest: null,
			permissionsManifestLoadFailed: false,
			versionInformation: versionStore.current ?? toAppVersionInformation({}),
			queryClient,
			swarmEnabled: undefined
		};
	}

	const versionInformationRequest = versionService.getVersionInformation();
	const autoLoginConfigRequest = browser
		? queryClient.query({
				queryKey: queryKeys.auth.autoLoginConfig(),
				queryFn: () => authService.getAutoLoginConfig()
			})
		: Promise.resolve(null);
	let [user, autoLoginConfig] = await Promise.all([
		// Only an explicit rejection signs the user out; transient check failures keep the known user.
		tryCatch(userService.getCurrentUser()).then((result): User | null => {
			if (!result.error) return result.data;
			return isAuthRejectionError(result.error) || isSessionCancelledError(result.error) ? null : userStore.current;
		}),
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
	let swarmEnabled: boolean | undefined;
	let permissionsManifest: PermissionsManifest | null = null;
	let permissionsManifestLoadFailed = false;
	if (user) {
		// Initialize environment store (required for settings service)
		const environmentsRequest = tryCatch(environmentManagementService.getEnvironments({ pagination: { page: 1, limit: -1 } }));
		const permissionsManifestRequest = dataOrNull(roleService.getPermissionsManifest());
		const environments = await environmentsRequest;
		if (!environments.error) {
			await environmentStore.initialize(environments.data.data);
		} else if (!environmentStore.isInitialized()) {
			// A failed refetch on an already-initialised store keeps the current
			// selection; re-initialising with [] would drop it to "No Environment".
			await environmentStore.initialize([]);
		}

		const settingsRequest = (
			userHasPermission(user, 'settings:read') ? dataOrNull(settingsService.getSettings()) : Promise.resolve(null)
		).then((loaded) => loaded ?? dataOrNull(settingsService.getPublicSettings(environmentStore.selected?.id ?? '0')));
		featureStore.connect(queryClient);
		const currentEnvironmentId = await environmentStore.getCurrentEnvironmentId();
		const featuresRequest = featureStore.refresh(currentEnvironmentId);
		// Optional discovery: skip it without swarm:read and never toast a denial.
		const swarmStatusRequest = userHasPermission(user, 'swarm:read', currentEnvironmentId)
			? dataOrNull(swarmService.getSwarmStatus(currentEnvironmentId, { suppressAccessDeniedToast: true }))
			: Promise.resolve(null);
		const [loadedSettings, loadedSwarmStatus, loadedPermissionsManifest] = await Promise.all([
			settingsRequest,
			swarmStatusRequest,
			permissionsManifestRequest
		]);
		// Keep recovery/settings pages reachable while a selected agent is offline.
		settings = loadedSettings ?? (await dataOrNull(settingsService.getPublicSettings()));
		await featuresRequest;
		swarmEnabled = loadedSwarmStatus?.enabled;
		permissionsManifest = loadedPermissionsManifest;
		permissionsManifestLoadFailed = loadedPermissionsManifest === null;
	} else {
		// Initialize empty environment store for unauthenticated users
		await environmentStore.initialize([]);

		// Try to fetch public settings for login page configuration
		settings = await dataOrNull(settingsService.getPublicSettings());
	}

	if (settings) {
		settingsStore.set(settings);
	}

	const versionInformation = await versionInformationRequest;
	versionStore.seed(versionInformation);
	// Waiting lets the first page view be traced, but a stalled SDK download must never hold up the app.
	await Promise.race([
		setTelemetry(
			{
				traces: settings?.frontendTracingEnabled ?? false,
				metrics: settings?.frontendMetricsEnabled ?? false,
				logs: settings?.frontendLogsEnabled ?? false
			},
			versionInformation.currentVersion
		),
		new Promise((resolve) => setTimeout(resolve, 2_000))
	]);

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
