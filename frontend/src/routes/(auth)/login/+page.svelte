<script lang="ts">
	import {
		browserSupportsWebAuthn,
		startAuthentication,
		type PublicKeyCredentialRequestOptionsJSON
	} from '@simplewebauthn/browser';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as InputGroup from '#lib/components/ui/input-group/index.js';
	import { AlertIcon, ApiKeyIcon, LockIcon, UserIcon, GithubIcon, OpenIdIcon } from '#lib/icons';
	import { goto, refreshAll } from '$app/navigation';
	import userStore from '#lib/stores/user-store';
	import { m } from '#lib/paraglide/messages';
	import { authService, MFARequiredError } from '#lib/services/auth-service';
	import { passkeyService } from '#lib/services/passkey-service';
	import type { AuthenticationResponse, MFAChallenge as MFAChallengeData } from '#lib/types/auth';
	import { normalizeAuthenticationError } from '#lib/utils/auth';
	import { getEffectiveLandingPage } from '#lib/utils/navigation';
	import { queryKeys } from '#lib/query/query-keys';
	import { getApplicationLogo } from '#lib/utils/docker';
	import { accentColorPreviewStore } from '#lib/utils/theme';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import AuthAmbient from '#lib/components/auth/auth-ambient.svelte';
	import MFAChallenge from '#lib/components/auth/mfa-challenge.svelte';
	import { onMount } from 'svelte';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';

	let { data }: PageProps = $props();

	let error = $state<string | null>(null);
	let username = $state('');
	let password = $state('');
	let mfaChallenge = $state<MFAChallengeData | null>(null);
	let passkeySupported = $state(false);
	const queryClient = useQueryClient();

	const accentColor = $derived($accentColorPreviewStore);
	const animationsEnabled = $derived($userStore?.preferences?.animationsEnabled ?? true);
	const logoUrl = $derived(getApplicationLogo(true, accentColor, accentColor, { animated: animationsEnabled }));

	const oidcEnabledBySettings = $derived(data.settings?.oidcEnabled === true);
	const showOidcLoginButton = $derived(oidcEnabledBySettings);

	const localAuthEnabledBySettings = $derived(data.settings?.authLocalEnabled !== false);
	const showLocalLoginForm = $derived(localAuthEnabledBySettings);

	const oidcAutoRedirect = $derived(data.settings?.oidcAutoRedirectToProvider === true);

	const oidcProviderName = $derived(data.settings?.oidcProviderName || '');
	const oidcProviderLogoUrl = $derived(data.settings?.oidcProviderLogoUrl || '');
	const oidcButtonLabel = $derived(oidcProviderName || m.common_oidc());

	// Only an explicit redirect is handed to the OIDC flow. Resolving the account
	// landing page here would bake the signed-out default into the OIDC round
	// trip, where it would then outrank the user's saved preference.
	const oidcLoginHref = $derived(data.redirectTo ? `/oidc/login?redirect=${encodeURIComponent(data.redirectTo)}` : '/oidc/login');

	const oidcLoginMutation = createMutation(() => ({
		mutationFn: async () => {
			error = null;
			await goto(oidcLoginHref);
		}
	}));

	const loginMutation = createMutation(() => ({
		mutationFn: () => authService.login({ username, password }),
		onSuccess: async (user) => {
			await userStore.setUser(user);
			await queryClient.invalidateQueries({ queryKey: queryKeys.auth.all });
			const redirectTo = data.redirectTo || getEffectiveLandingPage();
			await goto(redirectTo, { replaceState: true });
		},
		onError: (err) => {
			if (err instanceof MFARequiredError) {
				mfaChallenge = err.challenge;
				error = null;
				return;
			}
			error = normalizeAuthenticationError(err, m.auth_unexpected_error()).message;
		}
	}));

	const passkeyLoginMutation = createMutation(() => ({
		mutationFn: async (): Promise<AuthenticationResponse> => {
			const challenge = await passkeyService.beginLogin();
			const credential = await startAuthentication({
				optionsJSON: challenge.options as unknown as PublicKeyCredentialRequestOptionsJSON
			});
			return passkeyService.finishLogin(challenge.ceremonyId, credential);
		},
		onSuccess: async (response) => {
			if (response.status === 'mfa_required' && response.mfa) {
				mfaChallenge = response.mfa;
				return;
			}
			await authService.completeAuthentication(response);
			await refreshAll();
			await queryClient.invalidateQueries({ queryKey: queryKeys.auth.all });
			await goto(data.redirectTo || getEffectiveLandingPage(), { replaceState: true });
		},
		onError: (err) => {
			if (err instanceof Error && err.name === 'NotAllowedError') {
				error = m.auth_passkey_cancelled();
				return;
			}
			error = normalizeAuthenticationError(err, m.auth_passkey_failed()).message;
		}
	}));

	const isLocalLoading = $derived(loginMutation.isPending);
	const isOidcLoading = $derived(oidcLoginMutation.isPending);
	const isPasskeyLoading = $derived(passkeyLoginMutation.isPending);
	const isAnyLoading = $derived(isLocalLoading || isOidcLoading || isPasskeyLoading);

	onMount(() => {
		passkeySupported = browserSupportsWebAuthn();
		if (oidcAutoRedirect && oidcEnabledBySettings && !data.error) {
			oidcLoginMutation.mutate();
		}
	});

	function handleOidcLogin() {
		oidcLoginMutation.mutate(undefined, {
			onError: () => {
				// Fallback to direct navigation when mutation fails unexpectedly
				void goto(oidcLoginHref);
			}
		});
	}

	function handleLogin(event: Event) {
		event.preventDefault();

		if (!username || !password) {
			error = m.auth_credentials_required();
			return;
		}

		error = null;
		loginMutation.mutate();
	}

	async function completeMFA(response: AuthenticationResponse) {
		await authService.completeAuthentication(response);
		await refreshAll();
		await queryClient.invalidateQueries({ queryKey: queryKeys.auth.all });
		await goto(data.redirectTo || getEffectiveLandingPage(), { replaceState: true });
	}

	const showProviderRow = $derived(showOidcLoginButton || passkeySupported);
	const showDivider = $derived(showProviderRow && showLocalLoginForm);
</script>

<svelte:head>
	<title>{m.layout_title()}</title>
</svelte:head>

<AuthAmbient />

<div class="relative z-[var(--arcane-z-raised)] flex min-h-dvh items-center justify-center p-6">
	<div class="w-full max-w-[400px]">
		<div class="flex flex-col items-center">
			<img class="logo h-12 w-auto sm:h-14" src={logoUrl} alt={m.layout_title()} />
			{#if data.versionInformation?.displayVersion}
				<span class="enter mt-3 font-mono text-[10px] tracking-[0.2em] text-muted-foreground/60 uppercase" style="--d: 500ms"
					>{data.versionInformation.displayVersion}</span
				>
			{/if}
		</div>

		<div class="panel enter mt-10 rounded-2xl border border-border/50 bg-card/40 p-6 backdrop-blur-xl sm:p-8" style="--d: 650ms">
			<div class="mb-7 text-center">
				<h1 class="text-2xl font-semibold tracking-tight">{m.welcome_back()}</h1>
				<p class="mt-1.5 text-sm text-muted-foreground">{m.auth_login_subtitle()}</p>
			</div>

			<div class="space-y-4">
				{#if data.error}
					<Alert.Root variant="destructive">
						<AlertIcon class="size-4" />
						<Alert.Title>{m.auth_login_problem_title()}</Alert.Title>
						<Alert.Description>
							{#if data.error === 'oidc_invalid_response'}
								{m.auth_oidc_invalid_response()}
							{:else if data.error === 'oidc_misconfigured'}
								{m.auth_oidc_misconfigured()}
							{:else if data.error === 'oidc_userinfo_failed'}
								{m.auth_oidc_userinfo_failed()}
							{:else if data.error === 'oidc_missing_sub'}
								{m.auth_oidc_missing_sub()}
							{:else if data.error === 'oidc_email_collision'}
								{m.auth_oidc_email_collision()}
							{:else if data.error === 'oidc_token_error'}
								{m.auth_oidc_token_error()}
							{:else if data.error === 'user_processing_failed'}
								{m.auth_user_processing_failed()}
							{:else if data.errorMessage}
								{data.errorMessage}
							{:else}
								{m.auth_unexpected_error()}
							{/if}
						</Alert.Description>
					</Alert.Root>
				{/if}

				{#if data.errorMessage && !data.error}
					<Alert.Root variant="destructive">
						<AlertIcon class="size-4" />
						<Alert.Title>{m.auth_login_problem_title()}</Alert.Title>
						<Alert.Description>{data.errorMessage}</Alert.Description>
					</Alert.Root>
				{/if}

				{#if error}
					<Alert.Root variant="destructive">
						<AlertIcon class="size-4" />
						<Alert.Title>{m.auth_failed_title()}</Alert.Title>
						<Alert.Description>{error}</Alert.Description>
					</Alert.Root>
				{/if}

				{#if mfaChallenge}
					<MFAChallenge
						challenge={mfaChallenge}
						onComplete={completeMFA}
						onCancel={() => {
							mfaChallenge = null;
							error = null;
						}}
					/>
				{:else}
					{#if !showLocalLoginForm && !showOidcLoginButton && !passkeySupported}
						<Alert.Root variant="destructive">
							<AlertIcon class="size-4" />
							<Alert.Title>{m.auth_no_login_methods_title()}</Alert.Title>
							<Alert.Description>{m.auth_no_login_methods_description()}</Alert.Description>
						</Alert.Root>
					{/if}

					{#if showLocalLoginForm}
						<form id="login-form" name="login" action="" method="post" onsubmit={handleLogin} class="space-y-4" autocomplete="on">
							<div class="space-y-2">
								<Label for="username" class="text-xs">{m.common_username()}</Label>
								<InputGroup.Root role={undefined}>
									<InputGroup.Addon role={undefined}>
										<UserIcon />
									</InputGroup.Addon>
									<InputGroup.Input
										id="username"
										name="username"
										type="text"
										autocomplete="username"
										aria-label={m.common_username()}
										required
										bind:value={username}
										placeholder={m.auth_username_placeholder()}
										disabled={isAnyLoading}
									/>
								</InputGroup.Root>
							</div>
							<div class="space-y-2">
								<Label for="password" class="text-xs">{m.common_password()}</Label>
								<InputGroup.Root role={undefined}>
									<InputGroup.Addon role={undefined}>
										<LockIcon />
									</InputGroup.Addon>
									<InputGroup.Input
										id="password"
										name="password"
										type="password"
										autocomplete="current-password"
										aria-label={m.common_password()}
										required
										bind:value={password}
										placeholder={m.auth_password_placeholder()}
										disabled={isAnyLoading}
									/>
								</InputGroup.Root>
							</div>
							<ArcaneButton type="submit" action="login" loading={isLocalLoading} disabled={isAnyLoading} hoverEffect="none" />
						</form>
					{/if}

					{#if showDivider}
						<div class="flex items-center gap-3 py-1 text-xs text-muted-foreground">
							<div class="h-px flex-1 bg-border/60"></div>
							<span>{m.auth_or_continue()}</span>
							<div class="h-px flex-1 bg-border/60"></div>
						</div>
					{/if}

					{#if showProviderRow}
						<div class="flex flex-wrap items-center gap-2">
							{#if showOidcLoginButton}
								<ArcaneButton
									action="oidc_login"
									hoverEffect="none"
									class="min-w-0 flex-1"
									onclick={() => handleOidcLogin()}
									loading={isOidcLoading}
									disabled={isAnyLoading}
									icon={null}
									customLabel=""
								>
									{#if oidcProviderLogoUrl}
										<img src={oidcProviderLogoUrl} alt="" class="size-4 object-contain" />
									{:else}
										<OpenIdIcon class="size-4" />
									{/if}
									<span class="truncate">{oidcButtonLabel}</span>
								</ArcaneButton>
							{/if}

							{#if passkeySupported}
								<ArcaneButton
									action="login"
									icon={ApiKeyIcon}
									customLabel={m.common_passkey()}
									hoverEffect="none"
									class="min-w-0 flex-1"
									loading={isPasskeyLoading}
									disabled={isAnyLoading}
									onclick={() => {
										error = null;
										passkeyLoginMutation.mutate();
									}}
								/>
							{/if}
						</div>
					{/if}
				{/if}
			</div>
		</div>

		<div class="enter mt-8 flex justify-center" style="--d: 900ms">
			<a
				href="https://github.com/ofkm/arcane"
				target="_blank"
				rel="noopener noreferrer"
				class="inline-flex items-center gap-1.5 text-xs text-muted-foreground/70 transition-colors hover:text-foreground"
			>
				<GithubIcon class="size-3.5" />
				{m.common_view_on_github()}
			</a>
		</div>
	</div>
</div>

<style>
	/* Page elements rise in staggered behind the logo's trace-and-fill draw,
	   so the form appears as the wordmark finishes filling. */
	.enter {
		opacity: 0;
		animation: rise 0.6s cubic-bezier(0.22, 1, 0.36, 1) both;
		animation-delay: var(--d, 0ms);
	}

	@keyframes rise {
		from {
			opacity: 0;
			transform: translateY(10px);
		}
		to {
			opacity: 1;
			transform: none;
		}
	}

	.logo {
		filter: drop-shadow(0 0 28px color-mix(in oklab, var(--primary) 45%, transparent));
	}

	/* Accent hairline along the panel's top edge, echoing the logo trace. */
	.panel {
		position: relative;
		overflow: hidden;
	}

	.panel::before {
		content: '';
		position: absolute;
		top: 0;
		right: 1.5rem;
		left: 1.5rem;
		height: 1px;
		background: linear-gradient(90deg, transparent, color-mix(in oklab, var(--primary) 60%, transparent), transparent);
	}

	@media (prefers-reduced-motion: reduce) {
		.enter {
			animation: none;
			opacity: 1;
			transform: none;
		}
	}
</style>
