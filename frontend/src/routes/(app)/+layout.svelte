<script lang="ts">
	import { browser } from '$app/env';
	import { goto, afterNavigate } from '$app/navigation';
	import { page } from '$app/state';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	import ActivityCenter from '#lib/components/activity/activity-center.svelte';
	import MobileNav from '#lib/components/mobile-nav/mobile-nav.svelte';
	import OperationWatchDialog from '#lib/components/operation-watch-dialog.svelte';
	import AppSidebar from '#lib/components/sidebar/sidebar.svelte';
	import * as Sidebar from '#lib/components/ui/sidebar/index.js';
	import { navigationItems, filterByPermissions, type NavigationItem } from '#lib/config/navigation-config.js';
	import { IsMobile } from '#lib/hooks/is-mobile.svelte.js';
	import { IsTablet } from '#lib/hooks/is-tablet.svelte.js';
	import { m } from '#lib/paraglide/messages.js';
	import BaseAPIService from '#lib/services/api-service.js';
	import { environmentStatusStore } from '#lib/stores/environment-status.store.svelte.js';
	import { featureStore } from '#lib/stores/features.store.svelte.js';
	import { upgradeJobStore } from '#lib/stores/upgrade-job.store.svelte.js';
	import userStore, { userHasPermissionInAnyEnvironment } from '#lib/stores/user-store.svelte.js';
	import { versionStore } from '#lib/stores/version.store.svelte.js';
	import { cn } from '#lib/utils.js';
	import { getAuthRedirectPath } from '#lib/utils/auth.js';
	import { getEffectiveLandingPage, getEffectiveNavigationSettings, setMobileNavigation } from '#lib/utils/navigation.js';
	import { isEditableTarget, matchesShortcutEvent } from '#lib/utils/navigation.js';

	import type { LayoutProps } from './$types';
	let { data, children }: LayoutProps = $props();

	const versionInformation = $derived(versionStore.current ?? data.versionInformation);
	const user = $derived(data.user);
	const permissionsManifest = $derived(data.permissionsManifest);
	const permissionsManifestLoadFailed = $derived(data.permissionsManifestLoadFailed);
	const swarmEnabled = $derived(data.swarmEnabled);
	const podmanStatuses = $derived(data.podmanStatuses);
	const canReadActivities = $derived(userHasPermissionInAnyEnvironment(user, 'activities:read'));

	const isMobile = new IsMobile();
	const isTablet = new IsTablet();

	const navigationSettings = $derived.by(() => {
		// Track the store, not the loader snapshot: saving a preference calls
		// userStore.setUser() without re-running load().
		void userStore.current;
		return getEffectiveNavigationSettings();
	});
	const navigationMode = $derived(navigationSettings.mode);
	let hiddenNavigationSettings = $state.raw<typeof navigationSettings>();
	setMobileNavigation({
		get settings() {
			return navigationSettings;
		},
		get visible() {
			return !isMobile.current || !navigationSettings.scrollToHide || hiddenNavigationSettings !== navigationSettings;
		},
		set visible(value: boolean) {
			hiddenNavigationSettings = value ? undefined : navigationSettings;
		}
	});
	const overviewItems = $derived(filterByPermissions(navigationItems.overviewItems, user ?? null, permissionsManifest));
	const deployItems = $derived(filterByPermissions(navigationItems.deployItems, user ?? null, permissionsManifest));
	const resourceItems = $derived(filterByPermissions(navigationItems.resourceItems, user ?? null, permissionsManifest));
	const adminItems = $derived(filterByPermissions(navigationItems.adminItems, user ?? null, permissionsManifest));
	const shortcutItems = $derived.by(() => {
		const items: NavigationItem[] = [...overviewItems, ...deployItems, ...resourceItems, ...adminItems];
		return flattenNavigationItems(items).filter((item) => item.shortcut?.length);
	});

	$effect(() => {
		if (!user) {
			return;
		}
		void environmentStatusStore.start();
		versionStore.start();
		return () => {
			environmentStatusStore.stop();
			versionStore.stop();
		};
	});

	$effect(() => {
		if (!versionStore.buildChanged) return;
		untrack(() => {
			if (upgradeJobStore.reloadsOnBuildChange) {
				void BaseAPIService.reloadForNewBuild();
				return;
			}
			toast.info(m.upgrade_build_changed_title(), {
				id: 'arcane-build-changed',
				description: m.upgrade_build_changed_description(),
				duration: Number.POSITIVE_INFINITY,
				action: {
					label: m.update_center_step_reload(),
					onClick: () => void BaseAPIService.reloadForNewBuild()
				}
			});
		});
	});

	const authRedirectPath = $derived(
		getAuthRedirectPath(page.url.pathname, user, permissionsManifest, permissionsManifestLoadFailed, getEffectiveLandingPage())
	);

	$effect(() => {
		if (authRedirectPath) {
			goto(authRedirectPath);
		}
	});

	if (browser) {
		afterNavigate((event) => {
			if (!event.from) {
				return;
			}

			if (isMobile.current || isTablet.current) {
				window.scrollTo({ top: 0, left: 0, behavior: 'auto' });
			}
		});
	}

	function handleNavigationShortcut(event: KeyboardEvent) {
		if (event.defaultPrevented) return;
		if (userStore.current?.preferences?.keyboardShortcutsEnabled === false) return;
		if (isMobile.current || isTablet.current) return;
		if (isEditableTarget(event.target)) return;

		const match = shortcutItems.find((item: NavigationItem) => item.shortcut && matchesShortcutEvent(item.shortcut, event));
		if (!match) return;

		event.preventDefault();
		goto(match.url);
	}

	function flattenNavigationItems(items: NavigationItem[]): NavigationItem[] {
		return items.flatMap((item) => [item, ...(item.items ? flattenNavigationItems(item.items) : [])]);
	}
</script>

<svelte:window
	onkeydown={handleNavigationShortcut}
	onfocus={() => void featureStore.refreshKnown()}
	ononline={() => void featureStore.refreshKnown()}
/>

<Sidebar.Provider class={isMobile.current ? 'h-auto min-h-dvh' : undefined}>
	{#if !isMobile.current}
		<AppSidebar {versionInformation} {user} {swarmEnabled} {podmanStatuses} {permissionsManifest} />
	{/if}

	<main class={cn('relative z-(--arcane-z-content)', isMobile.current ? 'flex-1' : 'h-dvh flex-1')}>
		<section
			class={isMobile.current
				? cn(
						'px-3',
						navigationMode === 'docked'
							? navigationSettings.scrollToHide
								? 'pt-5'
								: 'pt-5 pb-(--mobile-docked-nav-offset,calc(3.5rem+env(safe-area-inset-bottom)))'
							: navigationSettings.scrollToHide
								? 'py-5'
								: 'py-5 pb-(--mobile-floating-nav-offset,6rem)'
					)
				: 'h-full p-3 sm:p-5'}
		>
			{#key user?.id}
				{@render children()}
			{/key}
		</section>
	</main>

	{#if isMobile.current}
		<MobileNav {user} {versionInformation} {swarmEnabled} {podmanStatuses} {permissionsManifest} />
	{/if}
</Sidebar.Provider>

{#if canReadActivities}
	<ActivityCenter />
{/if}

<OperationWatchDialog />
