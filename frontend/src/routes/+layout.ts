import { browser } from '$app/env';
import { page } from '$app/state';
import { redirect } from '@sveltejs/kit';
import { QueryClient } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { isSessionCancelledError } from '#lib/services/api-service.js';
import { authService } from '#lib/services/auth-service.js';
import { environmentManagementService } from '#lib/services/env-mgmt-service.js';
import { roleService } from '#lib/services/role-service.js';
import { settingsService } from '#lib/services/settings-service.js';
import versionService, { toAppVersionInformation } from '#lib/services/version-service.js';
import { clientStream } from '#lib/stores/client-stream.svelte.js';
import settingsStore from '#lib/stores/config-store.svelte.js';
import { environmentStatusStore } from '#lib/stores/environment-status.store.svelte.js';
import { environmentStore, LOCAL_DOCKER_ENVIRONMENT_ID } from '#lib/stores/environment.store.svelte.js';
import { featureStore } from '#lib/stores/features.store.svelte.js';
import userStore from '#lib/stores/user-store.svelte.js';
import { versionStore } from '#lib/stores/version.store.svelte.js';
import type { SessionBootstrap } from '#lib/types/auth.js';
import type { Environment } from '#lib/types/environment.js';
import type { PermissionsManifest } from '#lib/types/role.js';
import type { ResolvedSettings } from '#lib/types/ui/settings.js';
import type { User } from '#lib/types/user.js';
import type { VersionInfo } from '#lib/types/version.js';
import { extractApiErrorStatus, isAuthRejectionError } from '#lib/utils/api.js';
import { getAuthRedirectPath } from '#lib/utils/auth.js';
import { isEnvironmentOnline } from '#lib/utils/docker.js';
import { getEffectiveLandingPage } from '#lib/utils/navigation.js';
import { setTelemetry } from '#lib/utils/telemetry.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { LayoutLoad } from './$types';

export const ssr = false;

const UNAVAILABLE_UPSTREAM_STATUSES = new Set([502, 503, 504]);

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
				return (status < 400 || (status >= 500 && !UNAVAILABLE_UPSTREAM_STATUSES.has(status))) && failureCount < 2;
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
// Query results last pushed into the stores, so a cached repeat never overwrites newer local or streamed state.
let appliedUser: User | undefined;
let appliedEnvironments: Environment[] | undefined;
let appliedSettings: ResolvedSettings | undefined;
let appliedVersion: VersionInfo | undefined;
let seededBootstrap: SessionBootstrap | undefined;

function dataOrNull<T>(promise: Promise<T>): Promise<T | null> {
	return tryCatch(promise).then((result) => (result.error ? null : result.data));
}

function afterDiscoveryTimeout(): Promise<undefined> {
	return new Promise((resolve) => setTimeout(() => resolve(undefined), SECONDARY_DISCOVERY_TIMEOUT));
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
			queryClient
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
			environmentStatusStore.start();
		}
		const settingsRequest = passwordChangePending
			? Promise.resolve(null)
			: dataOrNull(
					queryClient.query({
						...rootQuery,
						queryKey: queryKeys.settings.byEnvironment(LOCAL_DOCKER_ENVIRONMENT_ID),
						queryFn: () => settingsService.getSettingsForEnvironmentMerged(LOCAL_DOCKER_ENVIRONMENT_ID)
					})
				);
		const featuresRequest = Promise.race([
			Promise.all(
				environmentStore.available
					.filter((env) => env.enabled && !passwordChangePending)
					.map((env) =>
						isEnvironmentOnline(env) ? featureStore.load(env.id, rootQuery.staleTime === 0) : featureStore.markUnavailable(env.id)
					)
			),
			afterDiscoveryTimeout()
		]);
		const [loadedSettings, loadedPermissionsManifest] = await Promise.all([settingsRequest, permissionsManifestRequest]);
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
		queryClient
	};
};
