<script lang="ts">
	import { browser, dev } from '$app/environment';
	import { invalidateAll } from '$app/navigation';
	import { navigating, page } from '$app/state';
	import ConfirmDialog from '$lib/components/confirm-dialog/confirm-dialog.svelte';
	import FirstLoginPasswordDialog from '$lib/components/dialogs/first-login-password-dialog.svelte';
	import Error from '$lib/components/error.svelte';
	import LoadingIndicator from '$lib/components/loading-indicator.svelte';
	import { Toaster } from '$lib/components/ui/sonner/index.js';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import { IsMobile } from '$lib/hooks/is-mobile.svelte.js';
	import { IsTablet } from '$lib/hooks/is-tablet.svelte.js';
	import { m } from '$lib/paraglide/messages';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { cn } from '$lib/utils';
	import { QueryClientProvider } from '@tanstack/svelte-query';
	import { SvelteQueryDevtools } from '@tanstack/svelte-query-devtools';
	import { ModeWatcher } from 'mode-watcher';
	import type { Snippet } from 'svelte';
	import { onMount } from 'svelte';
	import '../app.css';
	import type { LayoutData } from './$types';

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

	const isMobile = new IsMobile();
	const isTablet = new IsTablet();
	const isNavigating = $derived(navigating.type !== null);

	const isAuthPage = $derived(
		String(page.url.pathname).startsWith('/login') ||
			String(page.url.pathname).startsWith('/logout') ||
			String(page.url.pathname).startsWith('/oidc')
	);

	const showPasswordChangeDialog = $derived(!!(data.user && data.user.requiresPasswordChange && !isAuthPage));

	function handlePasswordChangeSuccess() {
		invalidateAll();
	}

	const pageTitle = $derived(
		environmentStore.selected ? `${m.layout_title()} | ${environmentStore.selected.name}` : m.layout_title()
	);
</script>

<svelte:head><title>{pageTitle}</title></svelte:head>

<div class={cn('flex min-h-dvh flex-col', 'bg-transparent')}>
	{#if !settings && data.user}
		<Error message={m.error_occurred()} showButton={true} />
	{:else}
		<Tooltip.Provider>
			<QueryClientProvider client={data.queryClient}>
				{@render children()}
				<FirstLoginPasswordDialog open={showPasswordChangeDialog} onSuccess={handlePasswordChangeSuccess} />
				{#if dev}
					<SvelteQueryDevtools />
				{/if}
			</QueryClientProvider>
		</Tooltip.Provider>
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
