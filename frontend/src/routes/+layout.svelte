<script lang="ts">
	import { dev } from '$app/env';
	import { afterNavigate, beforeNavigate, refreshAll } from '$app/navigation';
	import { navigating, page } from '$app/state';
	import { QueryClientProvider } from '@tanstack/svelte-query';
	import { SvelteQueryDevtools } from '@tanstack/svelte-query-devtools';
	import { ModeWatcher } from 'mode-watcher';
	import { onMount } from 'svelte';

	import ConfirmDialog from '#lib/components/confirm-dialog/confirm-dialog.svelte';
	import FirstLoginPasswordDialog from '#lib/components/dialogs/first-login-password-dialog.svelte';
	import Error from '#lib/components/error.svelte';
	import LoadingIndicator from '#lib/components/loading-indicator.svelte';
	import { Toaster } from '#lib/components/ui/sonner/index.js';
	import * as Tooltip from '#lib/components/ui/tooltip/index.js';
	import { getNavigationTitleForPath } from '#lib/config/navigation-config.js';
	import { IsMobile } from '#lib/hooks/is-mobile.svelte.js';
	import { IsTablet } from '#lib/hooks/is-tablet.svelte.js';
	import { m } from '#lib/paraglide/messages.js';
	import { isAuthPagePath } from '#lib/services/api-service.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { finishNavigation, startPageView } from '#lib/utils/telemetry.js';

	import type { LayoutProps } from './$types';

	import './layout.css';

	let { data, children }: LayoutProps = $props();

	onMount(() => {
		if (!dev && 'serviceWorker' in navigator) {
			navigator.serviceWorker.register('/service-worker.js', { type: 'module' });
		}
	});

	beforeNavigate((navigation) => {
		if (!navigation.willUnload) startPageView(navigation.to?.route.id);
	});

	afterNavigate((navigation) => finishNavigation(navigation.to?.route.id, navigation.type));

	const isMobile = new IsMobile();
	const isTablet = new IsTablet();

	// Mirror the per-device layout onto <html> for CSS; `$effect.pre` sets it before first paint.
	$effect.pre(() => {
		document.documentElement.setAttribute('data-layout', isMobile.current ? 'mobile' : 'desktop');
	});

	const isAuthPage = $derived(isAuthPagePath(page.url.pathname));

	const showPasswordChangeDialog = $derived(
		!!data.user?.requiresPasswordChange && !isAuthPage && !settingsStore.autoLoginEnabled.current
	);

	const resourceName = $derived.by((): string | undefined => {
		switch (page.route.id) {
			case '/(app)/projects/[projectId]':
				return page.data['project']?.name;
			case '/(app)/containers/[containerId]':
				return page.data['container']?.name?.replace(/^\/+/, '');
			case '/(app)/volumes/[volumeName]':
				return page.params['volumeName'];
			case '/(app)/networks/[networkId]':
				return page.data['network']?.name;
			case '/(app)/images/[imageId]':
				return page.data['image']?.repo;
			default:
				return undefined;
		}
	});

	// Root layout owns the document title; nested <title> tags are never restored on unmount.
	const pageTitle = $derived(
		[m.layout_title(), getNavigationTitleForPath(page.url.pathname), resourceName, environmentStore.selected?.name]
			.filter(Boolean)
			.join(' | ')
	);
</script>

<svelte:head>
	{#if !isAuthPage}
		<title>{pageTitle}</title>
	{/if}
</svelte:head>

<QueryClientProvider client={data.queryClient}>
	<div class="flex min-h-dvh flex-col bg-transparent">
		{#if !data.settings && data.user && page.route.id !== '/(app)/environments/[id]'}
			<Error message={m.error_occurred()} showButton={true} />
		{:else}
			<Tooltip.Provider>
				{@render children()}
				<FirstLoginPasswordDialog open={showPasswordChangeDialog} onSuccess={() => refreshAll()} />
				{#if dev}
					<SvelteQueryDevtools />
				{/if}
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
	<LoadingIndicator active={navigating.type !== null} thickness="h-1.5" />
</QueryClientProvider>
