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

	let [user, versionInformation] = await Promise.all([
		// Only an explicit rejection signs the user out; transient check failures keep the known user.
		tryCatch(userService.getCurrentUser()).then((result): User | null => {
			if (!result.error) return result.data;
			return isAuthRejectionError(result.error) || isSessionCancelledError(result.error) ? null : userStore.current;
		}),
		versionService.getVersionInformation()
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
		const opening = !opened;
		opened = true;
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

		const settingsRequest = (
			userHasPermission(user, 'settings:read') ? dataOrNull(settingsService.getSettings()) : Promise.resolve(null)
		).then((loaded) => loaded ?? dataOrNull(settingsService.getPublicSettings(environmentStore.selected?.id ?? '0')));
		featureStore.connect(queryClient);
		const currentEnvironmentId = await environmentStore.getCurrentEnvironmentId();
		const featuresRequest = featureStore.refresh(currentEnvironmentId);
		// The all environments preview shows navigation for features in any shown environment. The others load
		// in the background, so an unreachable environment never stalls navigation.
		for (const env of environmentStore.shown) {
			if (env.id !== currentEnvironmentId) void featureStore.load(env.id);
		}
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
}
