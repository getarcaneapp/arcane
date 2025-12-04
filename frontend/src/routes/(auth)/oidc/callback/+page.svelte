<script lang="ts">
	import { onMount } from 'svelte';
	import { goto, invalidateAll } from '$app/navigation';
	import { page } from '$app/state';
	import { toast } from 'svelte-sonner';
	import userStore from '$lib/stores/user-store';
	import type { User } from '$lib/types/user.type';
	import { m } from '$lib/paraglide/messages';
	import settingsStore from '$lib/stores/config-store';
	import { settingsService } from '$lib/services/settings-service';
	import { authService } from '$lib/services/auth-service';

	let isProcessing = $state(true);
	let error = $state('');

	onMount(async () => {
		try {
			const code = page.url.searchParams.get('code');
			const stateFromUrl = page.url.searchParams.get('state');
			const errorParam = page.url.searchParams.get('error');
			const errorDescription = page.url.searchParams.get('error_description');

			const redirectTo = localStorage.getItem('oidc_redirect') || '/dashboard';
			localStorage.removeItem('oidc_redirect');

			if (errorParam) {
				let userMessage = m.auth_oidc_provider_error();
				if (errorParam === 'access_denied') {
					userMessage = m.auth_oidc_access_denied();
				} else if (errorParam === 'invalid_request') {
					userMessage = m.auth_oidc_invalid_request();
				}

				error = errorDescription || userMessage;
				setTimeout(() => goto('/login?error=oidc_provider_error'), 3000);
				isProcessing = false;
				return;
			}

			if (!code || !stateFromUrl) {
				error = m.auth_oidc_invalid_response();
				setTimeout(() => goto('/login?error=oidc_invalid_response'), 3000);
				isProcessing = false;
				return;
			}

			const authResult = await authService.handleCallback(code, stateFromUrl);

			if (!authResult.success) {
				let userMessage = m.auth_oidc_auth_failed();
				if (authResult.error?.includes('state')) {
					userMessage = m.auth_oidc_state_mismatch();
				} else if (authResult.error?.includes('expired')) {
					userMessage = m.auth_oidc_session_expired();
				}

				error = userMessage;
				setTimeout(() => goto('/login?error=oidc_auth_failed'), 3000);
				isProcessing = false;
				return;
			}

			if (authResult.user) {
				const user: User = {
					id: authResult.user.sub || authResult.user.email || '',
					username: authResult.user.preferred_username || authResult.user.email || '',
					email: authResult.user.email,
					displayName:
						authResult.user.name ||
						authResult.user.displayName ||
						authResult.user.given_name ||
						authResult.user.preferred_username ||
						authResult.user.email ||
						m.common_unknown(),
					roles: authResult.user.groups || ['user'],
					createdAt: new Date().toISOString()
				};

				userStore.setUser(user);

				async function finalizeLogin() {
					const settings = await settingsService.getSettings();
					settingsStore.set(settings);
					toast.success('Successfully logged in!');
					goto(redirectTo, { replaceState: true });
				}

				await invalidateAll();
				finalizeLogin();
			} else {
				error = m.auth_oidc_user_info_missing();
				setTimeout(() => goto('/login?error=oidc_user_info_missing'), 3000);
				isProcessing = false;
			}
		} catch (err: any) {
			console.error('OIDC callback error:', err);

			let userMessage = m.auth_oidc_callback_error();
			if (err.message?.includes('network') || err.message?.includes('timeout')) {
				userMessage = m.auth_oidc_network_error();
			}

			error = userMessage;
			setTimeout(() => goto('/login?error=oidc_callback_error'), 3000);
			isProcessing = false;
		}
	});
</script>

<svelte:head><title>{m.layout_title()}</title></svelte:head>

<div class="bg-background flex min-h-screen items-center justify-center">
	<div class="w-full max-w-md space-y-8">
		<div class="text-center">
			{#if isProcessing}
				<div class="border-primary mx-auto h-12 w-12 animate-spin rounded-full border-b-2"></div>
				<h2 class="mt-6 text-2xl font-bold">{m.auth_processing_login()}</h2>
				<p class="text-muted-foreground mt-2 text-sm">{m.auth_processing_login_description()}</p>
			{:else if error}
				<div class="text-destructive">
					<svg class="mx-auto h-12 w-12" fill="none" viewBox="0 0 24 24" stroke="currentColor">
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
