<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { createMutation } from '@tanstack/svelte-query';
	import { onMount } from 'svelte';

	import OidcStatusPanel from '#lib/components/oidc-status-panel.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { APIError } from '#lib/services/api-service.js';
	import { authService } from '#lib/services/auth-service.js';

	let error = $state('');

	const oidcLoginMutation = createMutation(() => ({
		mutationFn: async () => {
			const requested = page.url.searchParams.get('redirect') ?? '';
			const redirect = requested.startsWith('/') && !requested.startsWith('//') ? requested : '';

			const authUrl = await authService.getAuthUrl(redirect);
			if (!authUrl) {
				throw new Error('oidc_url_generation_failed');
			}

			if (redirect) {
				localStorage.setItem('oidc_redirect', redirect);
			} else {
				localStorage.removeItem('oidc_redirect');
			}
			window.location.href = authUrl;
		},
		onError: (err: unknown) => {
			const serverMessage = err instanceof APIError && err.response ? err.message.trim() : '';
			const text = err instanceof Error ? err.message : '';
			let fallback = m.auth_oidc_init_failed();
			let redirectError = 'oidc_init_failed';

			if (text === 'oidc_url_generation_failed') {
				fallback = m.auth_oidc_url_generation_failed();
				redirectError = 'oidc_url_generation_failed';
			} else if (text.includes('discovery')) {
				fallback = m.auth_oidc_misconfigured();
				redirectError = 'oidc_misconfigured';
			} else if (text.includes('network') || text.includes('timeout') || text.includes('fetch')) {
				fallback = m.auth_oidc_network_error();
				redirectError = 'oidc_network_error';
			}

			error = serverMessage || fallback;
			if (serverMessage) sessionStorage.setItem('oidc_login_error', serverMessage);
			setTimeout(() => goto(resolve(`/(auth)/login/[[mode]]?error=${redirectError}`, {})), 3000);
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
