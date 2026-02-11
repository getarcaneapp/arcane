<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { m } from '$lib/paraglide/messages';
	import { authService } from '$lib/services/auth-service';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { createMutation } from '@tanstack/svelte-query';

	let error = $state('');

	const oidcLoginMutation = createMutation(() => ({
		mutationFn: async () => {
			const redirect = page.url.searchParams.get('redirect') || '/dashboard';

			const authUrl = await authService.getAuthUrl(redirect);
			if (!authUrl) {
				throw new Error('oidc_url_generation_failed');
			}

			localStorage.setItem('oidc_redirect', redirect);
			window.location.href = authUrl;
		},
		onError: (err: any) => {
			console.error('OIDC login initiation error:', err);

			let userMessage = m.auth_oidc_init_failed();
			let redirectError = 'oidc_init_failed';

			if (err.message === 'oidc_url_generation_failed') {
				userMessage = m.auth_oidc_url_generation_failed();
				redirectError = 'oidc_url_generation_failed';
			} else if (err.message?.includes('discovery')) {
				userMessage = m.auth_oidc_misconfigured();
				redirectError = 'oidc_misconfigured';
			} else if (err.message?.includes('network') || err.message?.includes('timeout')) {
				userMessage = m.auth_oidc_network_error();
				redirectError = 'oidc_network_error';
			}

			error = userMessage;
			setTimeout(() => goto(`/login?error=${redirectError}`), 3000);
		}
	}));

	const isRedirecting = $derived(oidcLoginMutation.isPending && !error);

	onMount(() => {
		oidcLoginMutation.mutate();
	});
</script>

<svelte:head><title>{m.layout_title()}</title></svelte:head>

<div class="bg-background flex min-h-screen items-center justify-center">
	<div class="w-full max-w-md space-y-8">
		<div class="flex flex-col items-center text-center">
			{#if isRedirecting && !error}
				<Spinner class="text-primary size-12" />
				<h2 class="mt-6 text-2xl font-bold">{m.auth_oidc_redirecting_title()}</h2>
				<p class="text-muted-foreground mt-2 text-sm">{m.auth_oidc_redirecting_description()}</p>
			{:else if error}
				<div class="text-destructive flex flex-col items-center">
					<svg class="h-12 w-12" fill="none" viewBox="0 0 24 24" stroke="currentColor">
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							stroke-width="2"
							d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4c-.77-.833-1.732-.833-2.5 0L3.341 16.5c-.77.833.192 2.5 1.732 2.5z"
						/>
					</svg>
					<h2 class="mt-6 text-2xl font-bold">{m.auth_authentication_error_title()}</h2>
					<p class="mt-2 text-sm">{error}</p>
					<p class="text-muted-foreground mt-4 text-xs">{m.auth_redirecting_to_login()}</p>
				</div>
			{/if}
		</div>
	</div>
</div>
