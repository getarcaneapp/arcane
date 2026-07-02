<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { m } from '$lib/paraglide/messages';
	import { authService } from '$lib/services/auth-service';
	import OidcStatusPanel from '$lib/components/oidc-status-panel.svelte';
	import { createMutation } from '@tanstack/svelte-query';

	let {}: PageProps = $props();

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

<OidcStatusPanel
	busy={isRedirecting && !error}
	busyTitle={m.auth_oidc_redirecting_title()}
	busyDescription={m.auth_oidc_redirecting_description()}
	{error}
/>
