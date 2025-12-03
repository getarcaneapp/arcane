<script lang="ts">
	import '../app.css';
	import { ModeWatcher } from 'mode-watcher';
	import { Toaster } from '$lib/components/ui/sonner/index.js';
	import { navigating, page } from '$app/state';
	import ConfirmDialog from '$lib/components/confirm-dialog/confirm-dialog.svelte';
	import * as Sidebar from '$lib/components/ui/sidebar/index.js';
	import AppSidebar from '$lib/components/sidebar/sidebar.svelte';
	import { goto, afterNavigate } from '$app/navigation';
	import { getAuthRedirectPath } from '$lib/utils/redirect.util';
	import LoadingIndicator from '$lib/components/loading-indicator.svelte';
	import type { LayoutData } from './$types';
	import type { Snippet } from 'svelte';
	import Error from '$lib/components/error.svelte';
	import { m } from '$lib/paraglide/messages';
	import { IsMobile } from '$lib/hooks/is-mobile.svelte.js';
	import { IsTablet } from '$lib/hooks/is-tablet.svelte.js';
	import MobileNav from '$lib/components/mobile-nav/mobile-nav.svelte';
	import { getEffectiveNavigationSettings, navigationSettingsOverridesStore } from '$lib/utils/navigation.utils';
	import { browser, dev } from '$app/environment';
	import { onMount } from 'svelte';
	import settingsStore from '$lib/stores/config-store';
	import FirstLoginPasswordDialog from '$lib/components/dialogs/first-login-password-dialog.svelte';
	import { invalidateAll } from '$app/navigation';
	import { cn } from '$lib/utils';

	let {
		data,
		children
	}: {
		data: LayoutData;
		children: Snippet;
	} = $props();

	onMount(() => {
		if (!dev && browser && 'serviceWorker' in navigator) {
			navigator.serviceWorker.register('/service-worker.js');
		}
	});

	const versionInformation = $derived(data.versionInformation);
	const user = $derived(data.user);
	const settings = $derived(data.settings);
	let isGlassEnabled = $state(false);

	$effect(() => {
		isGlassEnabled = settings?.glassEffectEnabled ?? false;
	});

	// Apply glass-enabled class to body based on settings
	$effect(() => {
		if (browser && settings) {
			const enabled = $settingsStore?.glassEffectEnabled ?? settings.glassEffectEnabled ?? false;
			isGlassEnabled = enabled;
			if (enabled) {
				document.body.classList.add('glass-enabled');
			} else {
				document.body.classList.remove('glass-enabled');
			}
		}
	});

	const isMobile = new IsMobile();
	const isTablet = new IsTablet();
	const isNavigating = $derived(navigating.type !== null);
	const isLoginPage = $derived(
		String(page.url.pathname) === '/login' ||
			String(page.url.pathname).startsWith('/auth/login') ||
			String(page.url.pathname) === '/auth' ||
			String(page.url.pathname).includes('/login') ||
			String(page.url.pathname).includes('/callback')
	);
	let showPasswordChangeDialog = $state(false);

	$effect(() => {
		// Show password change dialog if user requires password change
		// Make it reactive to data changes
		if (data.user && data.user.requiresPasswordChange && !isLoginPage) {
			showPasswordChangeDialog = true;
		} else {
			showPasswordChangeDialog = false;
		}
	});

	function handlePasswordChangeSuccess() {
		invalidateAll();
	}

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

<svelte:head><title>{m.layout_title()}</title></svelte:head>

<div class={cn('flex min-h-dvh flex-col', isGlassEnabled ? 'bg-transparent' : 'bg-background')}>
	{#if !settings}
		<Error message={m.error_occurred()} showButton={true} />
	{:else if !isLoginPage}
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
				<main class="flex-1">
					<section class="p-5">
						{@render children()}
					</section>
				</main>
			</Sidebar.Provider>
		{/if}
	{:else}
		<main class="flex-1">
			{@render children()}
		</main>
	{/if}
</div>

<ModeWatcher disableTransitions={false} />
<Toaster
	position={isMobile.current || isTablet.current ? 'top-center' : 'bottom-right'}
	toastOptions={{
		classes: {
			toast: 'border border-primary/30!',
			title: 'text-foreground',
			description: 'text-muted-foreground',
			actionButton: 'bg-primary text-primary-foreground hover:bg-primary/90',
			cancelButton: 'bg-muted text-muted-foreground hover:bg-muted/80',
			closeButton: 'text-muted-foreground hover:text-foreground'
		}
	}}
/>
<ConfirmDialog />
<LoadingIndicator active={isNavigating} thickness="h-1.5" />
<FirstLoginPasswordDialog bind:open={showPasswordChangeDialog} onSuccess={handlePasswordChangeSuccess} />
