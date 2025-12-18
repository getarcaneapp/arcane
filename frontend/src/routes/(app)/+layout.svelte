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

	const isMobile = new IsMobile();
	const isTablet = new IsTablet();

	const navigationSettings = $derived.by(() => {
		settings;
		navigationSettingsOverridesStore.current;
		return getEffectiveNavigationSettings();
	});
	const navigationMode = $derived(navigationSettings.mode);

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
</script>

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
	<MobileNav {navigationSettings} {user} {versionInformation} />
{:else}
	<Sidebar.Provider>
		<AppSidebar {versionInformation} {user} />
		<main class="h-dvh flex-1 overflow-hidden">
			<section class="h-full p-3 sm:p-5">
				{@render children()}
			</section>
		</main>
	</Sidebar.Provider>
{/if}
