import { browser } from '$app/env';
import { invalidate } from '$app/navigation';
import { page } from '$app/state';
import { redirect } from '@sveltejs/kit';
import { QueryClient, type QueryKey } from '@tanstack/svelte-query';

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
import type { SessionBootstrap } from '#lib/types/auth.js';
import type { Environment } from '#lib/types/environment.js';
import { type PodmanStatus } from '#lib/types/podman.js';
import type { PermissionsManifest } from '#lib/types/role.js';
import type { SwarmRuntimeStatus } from '#lib/types/swarm.js';
import { PODMAN_STATUS_DEPENDENCY } from '#lib/types/ui/podman.js';
import type { ResolvedSettings } from '#lib/types/ui/settings.js';
import { SWARM_STATUS_DEPENDENCY } from '#lib/types/ui/swarm.js';
import type { User } from '#lib/types/user.js';
import type { VersionInfo } from '#lib/types/version.js';
import { extractApiErrorStatus, isAuthRejectionError } from '#lib/utils/api.js';
import { getAuthRedirectPath } from '#lib/utils/auth.js';
import { isEnvironmentOnline } from '#lib/utils/docker.js';
import { readableEnvironments, sumAcrossEnvironments } from '#lib/utils/environments.js';
import { getEffectiveLandingPage } from '#lib/utils/navigation.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { LayoutLoad } from './$types';

export const ssr = false;

const DEFAULT_CACHE_MINUTES = 10;

const queryClient = new QueryClient({
	defaultOptions: {
		queries: {
			enabled: browser,
			staleTime: 15_000,
			gcTime: DEFAULT_CACHE_MINUTES * 60_000,
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

// Navigations reuse root data for this long; a refresh of the page on screen always refetches.
const ROOT_STALE_TIME = 30_000;
const ROOT_DEPENDENCY = 'app:root';
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
let knownSwarmEnabled: boolean | undefined;
let knownSwarmScope = '';
// Query results last pushed into the stores, so a cached repeat never overwrites newer local or streamed state.
let appliedUser: User | undefined;
let appliedEnvironments: Environment[] | undefined;
let appliedSettings: ResolvedSettings | undefined;
let appliedVersion: VersionInfo | undefined;
let seededBootstrap: SessionBootstrap | undefined;
let rootRevalidated = false;

type RootQuery<T> = { queryKey: QueryKey; queryFn: () => Promise<T>; staleTime: number; retry: false };

function rootData<T>(options: RootQuery<T>, navigating: boolean): Promise<T> {
	const cached = navigating ? queryClient.getQueryData<T>(options.queryKey) : undefined;
	if (cached === undefined) return queryClient.query(options);
	void tryCatch(queryClient.query(options)).then((result) => {
		const rejected = result.error !== null && (isAuthRejectionError(result.error) || isSessionCancelledError(result.error));
		if (!rejected && (result.error !== null || result.data === cached)) return;
		rootRevalidated = true;
		void invalidate(ROOT_DEPENDENCY);
	});
	return Promise.resolve(cached);
}

function dataOrNull<T>(promise: Promise<T>): Promise<T | null> {
	return tryCatch(promise).then((result) => (result.error ? null : result.data));
}

function afterDiscoveryTimeout(): Promise<undefined> {
	return new Promise((resolve) => setTimeout(() => resolve(undefined), SECONDARY_DISCOVERY_TIMEOUT));
}

export const load: LayoutLoad = async ({ url, depends }) => {
	depends(PODMAN_STATUS_DEPENDENCY);
	depends(SWARM_STATUS_DEPENDENCY);
	depends(ROOT_DEPENDENCY);
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
			podmanStatuses: {},
			podmanUnavailableReasons: {}
		};
	}

	// Explicit refreshes (refreshAll, invalidateAll) re-run this load for the page on screen; navigations change the URL.
	const navigating = url.href !== page.url.href;
	const rootQuery = { staleTime: navigating || rootRevalidated ? ROOT_STALE_TIME : 0, retry: false as const };
	if (!navigating) rootRevalidated = false;
	const sessionResult = await tryCatch(
		rootData(
			{
				...rootQuery,
				queryKey: queryKeys.auth.sessionBootstrap(),
				queryFn: async () => {
					const loaded = await authService.getSessionBootstrap();
					return { ...loaded, version: toAppVersionInformation(loaded.version) };
				}
			},
			navigating
		)
	);
	// Only an explicit rejection signs the user out; transient check failures keep the known user.
	const sessionRejected =
		sessionResult.error !== null && (isAuthRejectionError(sessionResult.error) || isSessionCancelledError(sessionResult.error));
	// A rejected session must not be served from the cache by the next load.
	if (sessionRejected) queryClient.removeQueries({ queryKey: queryKeys.auth.sessionBootstrap() });
	const bootstrap = sessionResult.data;
	let user: User | null = bootstrap?.user ?? (sessionRejected ? null : userStore.current);
	const versionInformation =
		bootstrap?.version ??
		(await dataOrNull(
			queryClient.query({
				...rootQuery,
				queryKey: queryKeys.system.appVersion(),
				queryFn: () => versionService.getVersionInformation()
			})
		)) ??
		versionStore.current ??
		toAppVersionInformation({});
	const autoLoginConfig =
		browser && versionInformation.enabledFeatures?.includes('autologin')
			? await dataOrNull(
					queryClient.query({
						queryKey: queryKeys.auth.autoLoginConfig(),
						queryFn: () => authService.getAutoLoginConfig()
					})
				)
			: null;

	settingsStore.autoLoginEnabled.set(autoLoginConfig?.enabled === true);
	if (autoLoginConfig?.enabled && !user) {
		user = await dataOrNull(
			queryClient.query({
				queryKey: queryKeys.auth.autoLoginAttempt(),
				queryFn: () => authService.attemptAutoLogin()
			})
		);
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
	}
	seededBootstrap = bootstrap ?? undefined;

	let settings = null;
	let swarmEnabled: boolean | undefined;
	let podmanStatuses: Record<string, PodmanStatus> = {};
	let podmanUnavailableReasons: Record<string, string> = {};
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
			queryClient.query({
				retry: false,
				staleTime: Infinity,
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

		const passwordChangePending = user.requiresPasswordChange && !settingsStore.autoLoginEnabled.current;
		if (!passwordChangePending) {
			featureStore.connect(queryClient);
			clientStream.connect(queryClient);
		}
		const settingsRequest = passwordChangePending
			? Promise.resolve(null)
			: dataOrNull(
					rootData(
						{
							...rootQuery,
							queryKey: queryKeys.settings.byEnvironment(LOCAL_DOCKER_ENVIRONMENT_ID),
							queryFn: () => settingsService.getSettingsForEnvironmentMerged(LOCAL_DOCKER_ENVIRONMENT_ID)
						},
						navigating
					)
				);
		const featureEnvironments = environmentStore.available.filter((env) => env.enabled && !passwordChangePending);
		const featuresRequest = Promise.race([
			Promise.all([
				featureStore.load(
					featureEnvironments.filter(isEnvironmentOnline).map((env) => env.id),
					rootQuery.staleTime === 0
				),
				...featureEnvironments.filter((env) => !isEnvironmentOnline(env)).map((env) => featureStore.markUnavailable(env.id))
			]),
			afterDiscoveryTimeout()
		]);
		const swarmEnvironments = passwordChangePending ? [] : readableEnvironments('swarm:read');
		const swarmStatusRequest =
			swarmEnvironments.length > 0
				? dataOrNull(
						rootData(
							{
								...rootQuery,
								queryKey: queryKeys.swarm.status(swarmEnvironments.map((env) => env.id).join(',')),
								queryFn: () =>
									sumAcrossEnvironments(
										swarmEnvironments,
										(scope, config) => swarmService.getSwarmStatus(scope, config),
										EMPTY_SWARM_STATUS
									)
							},
							navigating
						)
					)
				: Promise.resolve(null);
		const swarmScope = `${user.id}:${swarmEnvironments.map((env) => env.id).join(',')}`;
		if (swarmScope !== knownSwarmScope) {
			knownSwarmScope = swarmScope;
			knownSwarmEnabled = undefined;
		}
		const swarmStatusDiscovery = Promise.race([swarmStatusRequest, afterDiscoveryTimeout()]);
		const podmanEnvironments = passwordChangePending ? [] : readableEnvironments('podman:read');
		const podmanStatusRequest = (
			podmanEnvironments.length > 0
				? dataOrNull(
						rootData(
							{
								...rootQuery,
								queryKey: queryKeys.podman.status(podmanEnvironments.map((env) => env.id).join(',')),
								queryFn: () =>
									sumAcrossEnvironments(
										podmanEnvironments,
										(scope, config) => podmanService.getStatus(scope, config),
										EMPTY_PODMAN_STATUS
									)
							},
							navigating
						)
					)
				: Promise.resolve(null)
		).then((loaded) =>
			Object.fromEntries(
				(loaded?.byEnvironment ?? []).flatMap(({ environment, data }) =>
					data.detected || data.reason ? [[environment.id, data] as const] : []
				)
			)
		);
		const podmanStatusDiscovery = Promise.race([podmanStatusRequest, afterDiscoveryTimeout()]);
		const [loadedSettings, discoveredSwarmStatus, discoveredPodmanStatuses, loadedPermissionsManifest] = await Promise.all([
			settingsRequest,
			swarmStatusDiscovery,
			podmanStatusDiscovery,
			permissionsManifestRequest
		]);
		if (discoveredSwarmStatus === undefined) {
			void swarmStatusRequest.then((status) => {
				if (swarmScope !== knownSwarmScope || status?.enabled === knownSwarmEnabled) return;
				knownSwarmEnabled = status?.enabled;
				void invalidate(SWARM_STATUS_DEPENDENCY);
			});
		}
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
					queryFn: () => settingsService.getPublicSettingsForEnvironment(LOCAL_DOCKER_ENVIRONMENT_ID)
				})
			));
		await featuresRequest;
		if (discoveredSwarmStatus === undefined) {
			swarmEnabled = swarmScope === knownSwarmScope ? knownSwarmEnabled : undefined;
		} else {
			swarmEnabled = discoveredSwarmStatus?.enabled;
			if (swarmScope === knownSwarmScope) {
				knownSwarmEnabled = swarmEnabled;
			}
		}
		knownPodmanStatuses = discoveredPodmanStatuses ?? knownPodmanStatuses;
		podmanStatuses = Object.fromEntries(Object.entries(knownPodmanStatuses).filter(([, status]) => status.detected));
		podmanUnavailableReasons = Object.fromEntries(
			Object.entries(knownPodmanStatuses).flatMap(([id, status]) =>
				!status.detected && status.reason ? [[id, status.reason] as const] : []
			)
		);
		permissionsManifest = loadedPermissionsManifest;
		permissionsManifestLoadFailed = loadedPermissionsManifest === null;
	} else {
		await environmentStore.initialize([]);

		settings = await dataOrNull(
			queryClient.query({
				...rootQuery,
				queryKey: queryKeys.settings.public(LOCAL_DOCKER_ENVIRONMENT_ID),
				queryFn: () => settingsService.getPublicSettingsForEnvironment(LOCAL_DOCKER_ENVIRONMENT_ID)
			})
		);
	}

	// Settings saves and the version stream update the stores directly, so only new results replace them.
	if (settings && settings !== appliedSettings) {
		settingsStore.set(settings);
		appliedSettings = settings;
	}
	queryClient.setDefaultOptions({
		queries: {
			...queryClient.getDefaultOptions().queries,
			gcTime: (Number(settingsStore.current?.frontendCacheMinutes) || DEFAULT_CACHE_MINUTES) * 60_000
		}
	});
	if (versionInformation !== appliedVersion) {
		versionStore.seed(versionInformation);
		appliedVersion = versionInformation;
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
		podmanStatuses,
		podmanUnavailableReasons
	};
};
