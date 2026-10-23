import { browser } from '$app/env';
import { invalidate } from '$app/navigation';
import { page } from '$app/state';
import { redirect } from '@sveltejs/kit';
import { QueryClient } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { isSessionCancelledError } from '#lib/services/api-service.js';
import { authService } from '#lib/services/auth-service.js';
import { environmentManagementService } from '#lib/services/env-mgmt-service.js';
import { podmanService } from '#lib/services/podman-service.js';
import { roleService } from '#lib/services/role-service.js';
import { settingsService } from '#lib/services/settings-service.js';
import { swarmService } from '#lib/services/swarm-service.js';
import versionService, { toAppVersionInformation } from '#lib/services/version-service.js';
import { clientStream } from '#lib/stores/client-stream.svelte.js';
import settingsStore from '#lib/stores/config-store.svelte.js';
import { environmentStore, LOCAL_DOCKER_ENVIRONMENT_ID } from '#lib/stores/environment.store.svelte.js';
import { featureStore } from '#lib/stores/features.store.svelte.js';
import userStore from '#lib/stores/user-store.svelte.js';
import { versionStore } from '#lib/stores/version.store.svelte.js';
import type { PermissionsManifest, SessionBootstrap, User } from '#lib/types/auth.js';
import type { Environment } from '#lib/types/environment.js';
import { PODMAN_STATUS_DEPENDENCY, type PodmanStatus } from '#lib/types/podman.js';
import type { AppVersionInformation, ResolvedSettings } from '#lib/types/settings.js';
import type { SwarmRuntimeStatus } from '#lib/types/swarm.js';
import { extractApiErrorStatus, isAuthRejectionError } from '#lib/utils/api.js';
import { getAuthRedirectPath, userHasPermission } from '#lib/utils/auth.js';
import { isEnvironmentOnline } from '#lib/utils/docker.js';
import { readableEnvironments, sumAcrossEnvironments } from '#lib/utils/environments.js';
import { getEffectiveLandingPage } from '#lib/utils/navigation.js';
import { setTelemetry } from '#lib/utils/telemetry.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { LayoutLoad } from './$types';

export const ssr = false;

const queryClient = new QueryClient({
	defaultOptions: {
		queries: {
			enabled: browser,
			staleTime: 15_000,
			gcTime: 60 * 1000,
			refetchOnMount: true,
			refetchOnWindowFocus: true,
			refetchOnReconnect: 'always',
			retry: (failureCount, error) => {
				const status = extractApiErrorStatus(error, 0);
				return (status < 400 || status >= 500) && failureCount < 2;
			}
		}
	}
});

let authenticatedUserId: string | null | undefined;
let telemetryAwaited = false;

// Navigations reuse root data for this long; a refresh of the page on screen always refetches.
const ROOT_STALE_TIME = 30_000;
const SECONDARY_DISCOVERY_TIMEOUT = 1_500;
const ENVIRONMENT_LIST_OPTIONS = { pagination: { page: 1, limit: -1 } };
const EMPTY_SWARM_STATUS: SwarmRuntimeStatus = { enabled: false };
const EMPTY_PODMAN_STATUS: PodmanStatus = {
	detected: false,
	podCount: 0,
	rootless: false,
	quadletsSupported: false,
	quadletAppsSupported: false,
	autoUpdateMode: 'unavailable',
	systemdAvailable: false
};
let knownPodmanStatuses: Record<string, PodmanStatus> = {};
// Query results last pushed into the stores, so a cached repeat never overwrites newer local or streamed state.
let appliedUser: User | undefined;
let appliedEnvironments: Environment[] | undefined;
let appliedSettings: ResolvedSettings | undefined;
let appliedVersion: AppVersionInformation | undefined;
let seededBootstrap: SessionBootstrap | undefined;

function dataOrNull<T>(promise: Promise<T>): Promise<T | null> {
	return tryCatch(promise).then((result) => (result.error ? null : result.data));
}

function afterDiscoveryTimeout(): Promise<undefined> {
	return new Promise((resolve) => setTimeout(() => resolve(undefined), SECONDARY_DISCOVERY_TIMEOUT));
}

export const load: LayoutLoad = async ({ url, depends }) => {
	depends(PODMAN_STATUS_DEPENDENCY);
	// Logout must not wait on, or be triggered by, authenticated requests; preloading runs this too.
	if (url.pathname === '/logout') {
		return {
			user: null,
			settings: settingsStore.current ?? null,
			permissionsManifest: null,
			permissionsManifestLoadFailed: false,
			versionInformation: versionStore.current ?? toAppVersionInformation({}),
			queryClient,
			swarmEnabled: undefined,
			podmanStatuses: {}
		};
	}

	// Explicit refreshes (refreshAll, invalidateAll) re-run this load for the page on screen; navigations change the URL.
	const rootQuery = { staleTime: url.href === page.url.href ? 0 : ROOT_STALE_TIME, retry: false };
	const sessionResult = await tryCatch(
		queryClient.query({
			...rootQuery,
			queryKey: queryKeys.auth.sessionBootstrap(),
			queryFn: async () => {
				const loaded = await authService.getSessionBootstrap();
				return { ...loaded, version: toAppVersionInformation(loaded.version) };
			}
		})
	);
	// Only an explicit rejection signs the user out; transient check failures keep the known user.
	const sessionRejected =
		sessionResult.error !== null && (isAuthRejectionError(sessionResult.error) || isSessionCancelledError(sessionResult.error));
	// A rejected session must not be served from the cache by the next load.
	if (sessionRejected) queryClient.removeQueries({ queryKey: queryKeys.auth.sessionBootstrap() });
	const bootstrap = sessionResult.data;
	let user: User | null = bootstrap?.user ?? (sessionRejected ? null : userStore.current);
	const versionInformation = bootstrap
		? bootstrap.version
		: await queryClient.query({
				...rootQuery,
				queryKey: queryKeys.system.appVersion(),
				queryFn: () => versionService.getVersionInformation()
			});
	const autoLoginConfig =
		browser && versionInformation.enabledFeatures?.includes('autologin')
			? await queryClient.query({
					queryKey: queryKeys.auth.autoLoginConfig(),
					queryFn: () => authService.getAutoLoginConfig()
				})
			: null;

	settingsStore.autoLoginEnabled.set(autoLoginConfig?.enabled === true);
	if (autoLoginConfig?.enabled && !user) {
		user = await queryClient.query({
			queryKey: queryKeys.auth.autoLoginAttempt(),
			queryFn: () => authService.attemptAutoLogin()
		});
	}

	const nextAuthenticatedUserId = user?.id ?? null;
	if (authenticatedUserId !== undefined && authenticatedUserId !== nextAuthenticatedUserId) {
		authService.resetAuthenticatedState(queryClient, { restartMountedStores: user !== null });
	}
	authenticatedUserId = nextAuthenticatedUserId;
	if (!user) {
		userStore.clearUser();
	} else if (user !== appliedUser || !userStore.current) {
		await userStore.setUser(user);
	}
	appliedUser = user ?? undefined;
	if (bootstrap && bootstrap !== seededBootstrap) {
		queryClient.setQueryData(queryKeys.auth.currentUser(), bootstrap.user);
		queryClient.setQueryData(queryKeys.system.appVersion(), bootstrap.version);
		queryClient.setQueryData(queryKeys.environments.list(ENVIRONMENT_LIST_OPTIONS), bootstrap.environments);
		queryClient.setQueryData(queryKeys.roles.permissionsManifest(), bootstrap.permissionsManifest);
	}
	seededBootstrap = bootstrap ?? undefined;

	let settings = null;
	let swarmEnabled: boolean | undefined;
	let podmanStatuses: Record<string, PodmanStatus> = {};
	let permissionsManifest: PermissionsManifest | null = null;
	let permissionsManifestLoadFailed = false;
	if (user) {
		const environmentsRequest = tryCatch(
			bootstrap
				? Promise.resolve(bootstrap.environments)
				: queryClient.query({
						...rootQuery,
						queryKey: queryKeys.environments.list(ENVIRONMENT_LIST_OPTIONS),
						queryFn: () => environmentManagementService.getEnvironments(ENVIRONMENT_LIST_OPTIONS)
					})
		);
		const permissionsManifestRequest = dataOrNull(
			bootstrap
				? Promise.resolve(bootstrap.permissionsManifest)
				: queryClient.query({
						...rootQuery,
						queryKey: queryKeys.roles.permissionsManifest(),
						queryFn: () => roleService.getPermissionsManifest()
					})
		);
		const environments = await environmentsRequest;
		if (!environments.error) {
			// The live environment stream keeps the store newer than a cached list.
			if (environments.data.data !== appliedEnvironments) await environmentStore.initialize(environments.data.data);
			appliedEnvironments = environments.data.data;
		} else {
			await environmentStore.initialize(environmentStore.available);
		}

		featureStore.connect(queryClient);
		clientStream.connect(queryClient);
		const settingsRequest = userHasPermission(user, 'settings:read')
			? dataOrNull(
					queryClient.query({
						...rootQuery,
						queryKey: queryKeys.settings.byEnvironment(LOCAL_DOCKER_ENVIRONMENT_ID),
						queryFn: () => settingsService.getSettingsForEnvironmentMerged(LOCAL_DOCKER_ENVIRONMENT_ID)
					})
				)
			: Promise.resolve(null);
		const featuresRequest = Promise.race([
			Promise.all(
				environmentStore.available
					.filter((env) => env.enabled)
					.map((env) =>
						isEnvironmentOnline(env) ? featureStore.load(env.id, rootQuery.staleTime === 0) : featureStore.markUnavailable(env.id)
					)
			),
			afterDiscoveryTimeout()
		]);
		const swarmEnvironments = readableEnvironments('swarm:read');
		const swarmStatusRequest =
			swarmEnvironments.length > 0
				? dataOrNull(
						queryClient.query({
							...rootQuery,
							queryKey: queryKeys.swarm.status(swarmEnvironments.map((env) => env.id).join(',')),
							queryFn: () =>
								sumAcrossEnvironments(
									swarmEnvironments,
									(scope, config) => swarmService.getSwarmStatus(scope, config),
									EMPTY_SWARM_STATUS
								)
						})
					)
				: Promise.resolve(null);
		const podmanEnvironments = readableEnvironments('podman:read');
		const podmanStatusRequest = (
			podmanEnvironments.length > 0
				? dataOrNull(
						queryClient.query({
							...rootQuery,
							queryKey: queryKeys.podman.status(podmanEnvironments.map((env) => env.id).join(',')),
							queryFn: () =>
								sumAcrossEnvironments(
									podmanEnvironments,
									(scope, config) => podmanService.getStatus(scope, config),
									EMPTY_PODMAN_STATUS
								)
						})
					)
				: Promise.resolve(null)
		).then((loaded) =>
			Object.fromEntries(
				(loaded?.byEnvironment ?? []).flatMap(({ environment, data }) => (data.detected ? [[environment.id, data] as const] : []))
			)
		);
		const podmanStatusDiscovery = Promise.race([podmanStatusRequest, afterDiscoveryTimeout()]);
		const [loadedSettings, loadedSwarmStatus, discoveredPodmanStatuses, loadedPermissionsManifest] = await Promise.all([
			settingsRequest,
			swarmStatusRequest,
			podmanStatusDiscovery,
			permissionsManifestRequest
		]);
		if (discoveredPodmanStatuses === undefined) {
			void podmanStatusRequest.then((statuses) => {
				if (JSON.stringify(statuses) === JSON.stringify(knownPodmanStatuses)) return;
				knownPodmanStatuses = statuses;
				void invalidate(PODMAN_STATUS_DEPENDENCY);
			});
		}
		settings =
			loadedSettings ??
			(await dataOrNull(
				queryClient.query({
					...rootQuery,
					queryKey: queryKeys.settings.public(LOCAL_DOCKER_ENVIRONMENT_ID),
					queryFn: () => settingsService.getPublicSettings()
				})
			));
		await featuresRequest;
		swarmEnabled = loadedSwarmStatus?.enabled;
		podmanStatuses = discoveredPodmanStatuses ?? knownPodmanStatuses;
		knownPodmanStatuses = podmanStatuses;
		permissionsManifest = loadedPermissionsManifest;
		permissionsManifestLoadFailed = loadedPermissionsManifest === null;
	} else {
		// Initialize empty environment store for unauthenticated users
		await environmentStore.initialize([]);

		// Try to fetch public settings for login page configuration
		settings = await dataOrNull(
			queryClient.query({
				...rootQuery,
				queryKey: queryKeys.settings.public(LOCAL_DOCKER_ENVIRONMENT_ID),
				queryFn: () => settingsService.getPublicSettings()
			})
		);
	}

	// Settings saves and the version stream update the stores directly, so only new results replace them.
	if (settings && settings !== appliedSettings) {
		settingsStore.set(settings);
		appliedSettings = settings;
	}
	if (versionInformation !== appliedVersion) {
		versionStore.seed(versionInformation);
		appliedVersion = versionInformation;
	}

	const telemetry = setTelemetry(
		{
			traces: settings?.runtime.frontendTracingEnabled ?? false,
			metrics: settings?.runtime.frontendMetricsEnabled ?? false,
			logs: settings?.runtime.frontendLogsEnabled ?? false
		},
		versionInformation.currentVersion
	);
	// Waiting lets the first page view be traced, but a stalled SDK download must never hold up the app.
	if (!telemetryAwaited) {
		telemetryAwaited = true;
		let timeout: ReturnType<typeof setTimeout> | undefined;
		await Promise.race([telemetry, new Promise((resolve) => (timeout = setTimeout(resolve, 2_000)))]);
		clearTimeout(timeout);
	}

	const redirectPath = getAuthRedirectPath(
		url.pathname,
		user,
		permissionsManifest,
		permissionsManifestLoadFailed,
		getEffectiveLandingPage()
	);
	if (redirectPath && redirectPath !== url.pathname) {
		throw redirect(302, redirectPath);
	}

	return {
		// The store holds local account edits made since the cached user was fetched.
		user: user && userStore.current,
		settings: settings?.values ?? null,
		permissionsManifest,
		permissionsManifestLoadFailed,
		versionInformation,
		queryClient,
		swarmEnabled,
		podmanStatuses
	};
};
