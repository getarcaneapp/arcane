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
	import { queryKeys } from '$lib/query/query-keys';
	import { authService } from '$lib/services/auth-service';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';

	let error = $state('');
	const queryClient = useQueryClient();

	const buildLoginRedirect = (errorCode: string, message?: string) => {
		const params = new URLSearchParams({ error: errorCode });
		if (message) {
			params.set('message', message);
		}
		return `/login?${params.toString()}`;
	};

	type CallbackFailure = {
		code: string;
		userMessage: string;
	};

	function failure(code: string, userMessage: string): CallbackFailure {
		return { code, userMessage };
	}

	const callbackMutation = createMutation(() => ({
		mutationFn: async () => {
			const code = page.url.searchParams.get('code');
			const stateFromUrl = page.url.searchParams.get('state');
			const errorParam = page.url.searchParams.get('error');
			const errorDescription = page.url.searchParams.get('error_description');

			const redirectTo = localStorage.getItem('oidc_redirect') || '/dashboard';
			localStorage.removeItem('oidc_redirect');

			if (errorParam) {
				let userMessage = m.auth_oidc_provider_error();
				let redirectCode = 'oidc_provider_error';
				if (errorParam === 'access_denied') {
					userMessage = m.auth_oidc_access_denied();
					redirectCode = 'oidc_access_denied';
				} else if (errorParam === 'invalid_request') {
					userMessage = m.auth_oidc_invalid_request();
					redirectCode = 'oidc_invalid_request';
				}

				throw failure(redirectCode, errorDescription || userMessage);
			}

			if (!code || !stateFromUrl) {
				throw failure('oidc_invalid_response', m.auth_oidc_invalid_response());
			}

			const authResult = await authService.handleCallback(code, stateFromUrl);

			if (!authResult.success) {
				let userMessage = m.auth_oidc_auth_failed();
				if (authResult.error?.includes('state')) {
					userMessage = m.auth_oidc_state_mismatch();
				} else if (authResult.error?.includes('expired')) {
					userMessage = m.auth_oidc_session_expired();
				}

				throw failure('oidc_auth_failed', authResult.error || userMessage);
			}

			if (!authResult.user) {
				throw failure('oidc_user_info_missing', m.auth_oidc_user_info_missing());
			}

			return {
				authResult,
				redirectTo
			};
		},
		onSuccess: async ({ authResult, redirectTo }) => {
			const user: User = {
				id: authResult.user!.sub || authResult.user!.email || '',
				username: authResult.user!.preferred_username || authResult.user!.email || '',
				email: authResult.user!.email,
				displayName:
					authResult.user!.name ||
					authResult.user!.displayName ||
					authResult.user!.given_name ||
					authResult.user!.preferred_username ||
					authResult.user!.email ||
					m.common_unknown(),
				roles: authResult.user!.groups || ['user'],
				createdAt: new Date().toISOString()
			};

			userStore.setUser(user);
			await invalidateAll();
			const settings = await queryClient.fetchQuery({
				queryKey: queryKeys.settings.global(),
				queryFn: () => settingsService.getSettings()
			});
			settingsStore.set(settings);
			toast.success('Successfully logged in!');
			await goto(redirectTo, { replaceState: true });
		},
		onError: (err: unknown) => {
			console.error('OIDC callback error:', err);

			let redirectCode = 'oidc_callback_error';
			let userMessage: string = String(m.auth_oidc_callback_error());

			if (err && typeof err === 'object' && 'code' in err && 'userMessage' in err) {
				redirectCode = String((err as CallbackFailure).code);
				userMessage = String((err as CallbackFailure).userMessage);
			} else {
				const unknownError = err as { message?: string };
				if (unknownError?.message?.includes('network') || unknownError?.message?.includes('timeout')) {
					userMessage = String(m.auth_oidc_network_error());
					redirectCode = 'oidc_network_error';
				} else if (unknownError?.message && !unknownError.message.includes('Request failed')) {
					userMessage = unknownError.message;
				}
			}

			error = userMessage;
			setTimeout(() => goto(buildLoginRedirect(redirectCode, userMessage)), 3000);
		}
	}));

	const isProcessing = $derived(callbackMutation.isPending);

	onMount(() => {
		callbackMutation.mutate();
	});
</script>

<div class="bg-background flex min-h-screen items-center justify-center">
	<div class="w-full max-w-md space-y-8">
		<div class="flex flex-col items-center text-center">
			{#if isProcessing}
				<Spinner class="text-primary size-12" />
				<h2 class="mt-6 text-2xl font-bold">{m.auth_processing_login()}</h2>
				<p class="text-muted-foreground mt-2 text-sm">{m.auth_processing_login_description()}</p>
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
