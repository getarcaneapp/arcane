<script lang="ts">
	import '../app.css';
	import { ModeWatcher } from 'mode-watcher';
	import { Toaster } from '$lib/components/ui/sonner/index.js';
	import { navigating, page } from '$app/state';
	import ConfirmDialog from '$lib/components/confirm-dialog/confirm-dialog.svelte';
	import LoadingIndicator from '$lib/components/loading-indicator.svelte';
	import type { LayoutData } from './$types';
	import type { Snippet } from 'svelte';
	import Error from '$lib/components/error.svelte';
	import { m } from '$lib/paraglide/messages';
	import { IsMobile } from '$lib/hooks/is-mobile.svelte.js';
	import { IsTablet } from '$lib/hooks/is-tablet.svelte.js';
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

	const settings = $derived(data.settings);
	let isGlassEnabled = $state(false);

	$effect(() => {
		isGlassEnabled = settings?.glassEffectEnabled ?? false;
	});

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

	const isAuthPage = $derived(
		String(page.url.pathname).startsWith('/login') ||
			String(page.url.pathname).startsWith('/logout') ||
			String(page.url.pathname).startsWith('/oidc')
	);

	let showPasswordChangeDialog = $state(false);

	$effect(() => {
		if (data.user && data.user.requiresPasswordChange && !isAuthPage) {
			showPasswordChangeDialog = true;
		} else {
			showPasswordChangeDialog = false;
		}
	});

	function handlePasswordChangeSuccess() {
		invalidateAll();
	}
</script>

<svelte:head><title>{m.layout_title()}</title></svelte:head>

<div class={cn('flex min-h-dvh flex-col', isGlassEnabled ? 'bg-transparent' : 'bg-background')}>
	{#if !settings}
		<Error message={m.error_occurred()} showButton={true} />
	{:else}
		{@render children()}
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
