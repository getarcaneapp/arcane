import { browser } from '$app/env';
import { navigating, page } from '$app/state';
import { error, redirect } from '@sveltejs/kit';
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
import type { Environment } from '#lib/types/environment.js';
import type { AppVersionInformation, Settings } from '#lib/types/settings.js';
import { isAuthRejectionError } from '#lib/utils/api.js';
import { getAuthRedirectPath, shownEnvironmentForRoute, userHasPermission } from '#lib/utils/auth.js';
import { getEffectiveLandingPage } from '#lib/utils/navigation.js';
import { setTelemetry } from '#lib/utils/telemetry.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { LayoutLoad } from './$types';

export const ssr = false;

const spansEnvironments = ({ queryKey }: { queryKey: readonly unknown[] }) =>
	environmentStore.shown.length > 1 && queryKey.includes(environmentStore.shownKey);

const queryClient = new QueryClient({
	defaultOptions: {
		queries: {
			enabled: browser,
			// Covers the gap between a page loader's fetch and the component's
			// mount so the same query isn't fetched twice in one navigation.
			staleTime: (query) => (spansEnvironments(query) ? 15_000 : 2_000),
			gcTime: 60 * 1000,
			refetchOnMount: true,
			refetchOnWindowFocus: (query) => (spansEnvironments(query) ? true : 'always'),
			refetchOnReconnect: 'always'
		}
	}
});

let authenticatedUserId: string | null | undefined;
// Whether a signed-in page has loaded; later loads without a navigation to their path are preloads.
let opened = false;
let telemetryAwaited = false;

// Navigations reuse root data for this long; a refresh of the page on screen always refetches.
const ROOT_STALE_TIME = 30_000;
// Query results last pushed into the stores, so a cached repeat never overwrites newer local or streamed state.
let appliedUser: User | undefined;
let appliedEnvironments: Environment[] | undefined;
let appliedSettings: Settings | undefined;
let appliedVersion: AppVersionInformation | undefined;

function dataOrNull<T>(promise: Promise<T>): Promise<T | null> {
	return tryCatch(promise).then((result) => (result.error ? null : result.data));
}

export const load: LayoutLoad = async (event) => {
	// With several environments shown this load may switch environments for the route, and child loads start
	// alongside it, so their environment lookups wait until it has chosen.
	const releaseSelection = environmentStore.shown.length > 1 ? environmentStore.holdSelection() : undefined;
	try {
		return await loadRoot(event, releaseSelection);
	} finally {
		releaseSelection?.();
	}
};

async function loadRoot({ url }: Parameters<LayoutLoad>[0], releaseSelection: (() => void) | undefined) {
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

	// Explicit refreshes (refreshAll, invalidateAll) re-run this load for the page on screen; navigations change the URL.
	const rootQuery = { staleTime: url.href === page.url.href ? 0 : ROOT_STALE_TIME, retry: false };
	let [user, versionInformation] = await Promise.all([
		// Only an explicit rejection signs the user out; transient check failures keep the known user.
		tryCatch(
			queryClient.query({ ...rootQuery, queryKey: queryKeys.auth.currentUser(), queryFn: () => userService.getCurrentUser() })
		).then((result): User | null => {
			if (!result.error) return result.data;
			if (!isAuthRejectionError(result.error) && !isSessionCancelledError(result.error)) return userStore.current;
			// A rejected session must not be served from the cache by the next load.
			queryClient.removeQueries({ queryKey: queryKeys.auth.currentUser() });
			return null;
		}),
		queryClient.query({
			...rootQuery,
			queryKey: queryKeys.system.appVersion(),
			queryFn: () => versionService.getVersionInformation()
		})
	]);
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

	let settings = null;
	let swarmEnabled: boolean | undefined;
	let permissionsManifest: PermissionsManifest | null = null;
	let permissionsManifestLoadFailed = false;
	if (user) {
		const opening = !opened;
		opened = true;
		// Initialize environment store (required for settings service)
		const environmentListOptions = { pagination: { page: 1, limit: -1 } };
		const environmentsRequest = tryCatch(
			queryClient.query({
				...rootQuery,
				queryKey: queryKeys.environments.list(environmentListOptions),
				queryFn: () => environmentManagementService.getEnvironments(environmentListOptions)
			})
		);
		const permissionsManifestRequest = dataOrNull(
			queryClient.query({
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
		} else if (!environmentStore.isInitialized()) {
			// A failed refetch on an already-initialised store keeps the current
			// selection; re-initialising with [] would drop it to "No Environment".
			await environmentStore.initialize([]);
		}

		// With the preview on, a page only another shown environment allows opens there instead of failing.
		if (environmentStore.shown.length > 1) {
			const reachable = shownEnvironmentForRoute(
				url.pathname,
				user,
				environmentStore.selected?.id,
				await permissionsManifestRequest
			);
			const environment = environmentStore.available.find((env) => env.id === reachable?.id);
			// Switch only when opening, reloading or navigating to the page on screen. A hover preload of another page
			// must not retarget this one, so it is stopped instead; SvelteKit then drops it and the click loads afresh.
			const forPageOnScreen = opening || navigating.to?.url.pathname === url.pathname || page.url.pathname === url.pathname;
			if (environment && !forPageOnScreen) error(403);
			if (environment) await environmentStore.setEnvironment(environment, { navigate: false });
		}
		releaseSelection?.();

		featureStore.connect(queryClient);
		const currentEnvironmentId = await environmentStore.getCurrentEnvironmentId();
		const settingsRequest = (
			userHasPermission(user, 'settings:read')
				? dataOrNull(
						queryClient.query({
							...rootQuery,
							queryKey: queryKeys.settings.byEnvironment(currentEnvironmentId),
							queryFn: () => settingsService.getSettingsForEnvironmentMerged(currentEnvironmentId)
						})
					)
				: Promise.resolve(null)
		).then(
			(loaded) =>
				loaded ??
				dataOrNull(
					queryClient.query({
						...rootQuery,
						queryKey: queryKeys.settings.public(currentEnvironmentId),
						queryFn: () => settingsService.getPublicSettings(currentEnvironmentId)
					})
				)
		);
		const featuresRequest = featureStore.load(currentEnvironmentId, rootQuery.staleTime === 0);
		// The all environments preview shows navigation for features in any shown environment. The others load
		// in the background, so an unreachable environment never stalls navigation.
		for (const env of environmentStore.shown) {
			if (env.id !== currentEnvironmentId) void featureStore.load(env.id);
		}
		// Optional discovery: skip it without swarm:read and never toast a denial.
		const swarmStatusRequest = userHasPermission(user, 'swarm:read', currentEnvironmentId)
			? dataOrNull(
					queryClient.query({
						...rootQuery,
						queryKey: queryKeys.swarm.status(currentEnvironmentId),
						queryFn: () => swarmService.getSwarmStatus(currentEnvironmentId, { suppressAccessDeniedToast: true })
					})
				)
			: Promise.resolve(null);
		const [loadedSettings, loadedSwarmStatus, loadedPermissionsManifest] = await Promise.all([
			settingsRequest,
			swarmStatusRequest,
			permissionsManifestRequest
		]);
		// Keep recovery/settings pages reachable while a selected agent is offline.
		settings =
			loadedSettings ??
			(await dataOrNull(
				queryClient.query({
					...rootQuery,
					queryKey: queryKeys.settings.public('0'),
					queryFn: () => settingsService.getPublicSettings()
				})
			));
		await featuresRequest;
		swarmEnabled = loadedSwarmStatus?.enabled;
		permissionsManifest = loadedPermissionsManifest;
		permissionsManifestLoadFailed = loadedPermissionsManifest === null;
	} else {
		// Initialize empty environment store for unauthenticated users
		await environmentStore.initialize([]);

		// Try to fetch public settings for login page configuration
		settings = await dataOrNull(
			queryClient.query({
				...rootQuery,
				queryKey: queryKeys.settings.public('0'),
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
			traces: settings?.frontendTracingEnabled ?? false,
			metrics: settings?.frontendMetricsEnabled ?? false,
			logs: settings?.frontendLogsEnabled ?? false
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
		environmentStore.selected?.id || '0',
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
		settings,
		permissionsManifest,
		permissionsManifestLoadFailed,
		versionInformation,
		queryClient,
		swarmEnabled
	};
}
