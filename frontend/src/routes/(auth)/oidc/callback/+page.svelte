<script lang="ts">
	import { onMount } from 'svelte';
	import { goto, invalidateAll } from '$app/navigation';
	import { page } from '$app/state';
	import { toast } from 'svelte-sonner';
	import userStore from '$lib/stores/user-store';
	import type { User } from '$lib/types/auth';
	import { m } from '$lib/paraglide/messages';
	import settingsStore from '$lib/stores/config-store';
	import { settingsService } from '$lib/services/settings-service';
	import { queryKeys } from '$lib/query/query-keys';
	import { authService } from '$lib/services/auth-service';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { getAuthRedirectPath } from '$lib/utils/auth';
	import OidcStatusPanel from '$lib/components/oidc-status-panel.svelte';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';

	let {}: PageProps = $props();

	let error = $state('');
	const queryClient = useQueryClient();

	const buildLoginRedirect = (errorCode: string, message?: string) => {
		const params = new URLSearchParams({ error: errorCode });
		if (message) {
			params.set('message', message);
		}
		return `/login?${params.toString()}`;
	};

	const getStoredRedirect = () => {
		const storedRedirect = localStorage.getItem('oidc_redirect');
		return storedRedirect?.startsWith('/') && !storedRedirect.startsWith('//') ? storedRedirect : '/dashboard';
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

			const redirectTo = getStoredRedirect();
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
			// Build a placeholder user from the OIDC response. The real user
			// (with role assignments + permissions) is fetched by invalidateAll
			// below, which re-runs the root +layout.ts loader.
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
				roleAssignments: [],
				permissionsByEnv: {},
				isGlobalAdmin: false,
				createdAt: new Date().toISOString()
			};

			userStore.setUser(user);
			// invalidateAll re-runs the root +layout.ts loader, which fetches
			// settings (with a graceful catch). We don't fetch them here directly
			// — a user with zero/limited permissions would 403 on settings:read
			// and crash this handler, leaving them stuck on a white screen.
			await invalidateAll();
			try {
				const settings = await queryClient.fetchQuery({
					queryKey: queryKeys.settings.global(),
					queryFn: () => settingsService.getSettings()
				});
				settingsStore.set(settings);
			} catch (err) {
				// User lacks settings:read or settings fetch failed — not fatal;
				// the root layout already pulls public settings as a fallback.
				console.warn('Skipping post-login settings fetch:', err);
			}
			toast.success('Successfully logged in!');
			// Navigate straight to a route the user can actually reach. Computing the
			// reachable target here (rather than always going to /dashboard) avoids the
			// (app) layout's auth-redirect superseding this navigation: an interrupted
			// goto() never resolves, which would hang this callback on "Processing
			// Login…". Environment-scoped users (no global perms) would otherwise bounce
			// to /no-access mid-navigation. invalidateAll() above has repopulated
			// page.data (user + permissions manifest) and the environment store.
			const landingUser = page.data['user'] ?? user;
			const target =
				getAuthRedirectPath(
					redirectTo,
					landingUser,
					environmentStore.selected?.id,
					page.data['permissionsManifest'],
					page.data['permissionsManifestLoadFailed'] ?? false
				) ?? redirectTo;
			await goto(target, { replaceState: true });
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

<OidcStatusPanel
	busy={isProcessing}
	busyTitle={m.auth_processing_login()}
	busyDescription={m.auth_processing_login_description()}
	{error}
>
	<!-- Defensive fallback: if neither branch matches (e.g. an
	     unexpected throw inside onSuccess), the user would otherwise
	     see a blank page with no way out. Always show a logout link. -->
	<p class="text-muted-foreground text-sm">{m.auth_processing_login()}</p>
	<a href="/logout" class="text-primary mt-6 text-xs underline">{m.common_logout()}</a>
</OidcStatusPanel>
