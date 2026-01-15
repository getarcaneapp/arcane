<script lang="ts">
	import { page } from '$app/state';
	import { goto, afterNavigate } from '$app/navigation';
	import { getAuthRedirectPath } from '$lib/utils/redirect.util';
	import * as Sidebar from '$lib/components/ui/sidebar/index.js';
	import AppSidebar from '$lib/components/sidebar/sidebar.svelte';
	import MobileNav from '$lib/components/mobile-nav/mobile-nav.svelte';
	import { IsMobile } from '$lib/hooks/is-mobile.svelte.js';
	import { IsTablet } from '$lib/hooks/is-tablet.svelte.js';
	import { getEffectiveNavigationSettings, navigationSettingsOverridesStore } from '$lib/utils/navigation.utils';
	import { browser } from '$app/environment';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { navigationItems, getManagementItems, type NavigationItem } from '$lib/config/navigation-config';
	import { isEditableTarget, matchesShortcutEvent } from '$lib/utils/keyboard-shortcut.utils';
	import { cn } from '$lib/utils';
	import type { Snippet } from 'svelte';
	import type { LayoutData } from './$types';

	let {
		data,
		children
	}: {
		data: LayoutData;
		children: Snippet;
	} = $props();

	const versionInformation = $derived(data.versionInformation);
	const user = $derived(data.user);
	const settings = $derived(data.settings);
	const dockerInfo = $derived(data.dockerInfo);

	const isMobile = new IsMobile();
	const isTablet = new IsTablet();

	const navigationSettings = $derived.by(() => {
		settings;
		navigationSettingsOverridesStore.current;
		return getEffectiveNavigationSettings();
	});
	const navigationMode = $derived(navigationSettings.mode);
	const isAdmin = $derived(!!user?.roles?.includes('admin'));
	const currentEnvId = $derived(environmentStore.selected?.id || '0');
	const managementItems = $derived(getManagementItems(currentEnvId));
	const settingsShortcutItems = $derived.by(() => (isAdmin ? (navigationItems.settingsItems ?? []) : []));
	const shortcutItems = $derived.by(() => {
		const items: NavigationItem[] = [...managementItems, ...navigationItems.resourceItems, ...settingsShortcutItems];
		return flattenNavigationItems(items).filter((item) => item.shortcut?.length);
	});

	$effect(() => {
		const redirectPath = getAuthRedirectPath(page.url.pathname, user);
		if (redirectPath) {
			goto(redirectPath);
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
		if (settings?.keyboardShortcutsEnabled === false) return;
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

<svelte:window onkeydown={handleNavigationShortcut} />

{#if isMobile.current}
	<main class="flex-1">
		<section
			class={cn(
				'px-2',
				navigationMode === 'docked'
					? navigationSettings.scrollToHide
						? 'pt-5 sm:px-5 sm:pt-5'
						: 'pt-5 pb-(--mobile-docked-nav-offset,calc(3.5rem+env(safe-area-inset-bottom))) sm:p-5'
					: navigationSettings.scrollToHide
						? 'py-5 sm:p-5'
						: 'py-5 pb-(--mobile-floating-nav-offset,6rem) sm:p-5'
			)}
		>
			{@render children()}
		</section>
	</main>
	<MobileNav {navigationSettings} {user} {versionInformation} {dockerInfo} />
{:else}
	<Sidebar.Provider>
		<AppSidebar {versionInformation} {user} {dockerInfo} />
		<main class="h-dvh flex-1">
			<section class="h-full p-3 sm:p-5">
				{@render children()}
			</section>
		</main>
	</Sidebar.Provider>
{/if}
