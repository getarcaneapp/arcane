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
	import { getEffectiveLandingPage } from '#lib/utils/navigation';
	import { queryKeys } from '#lib/query/query-keys';
	import { getApplicationLogo } from '#lib/utils/docker';
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

	const logoUrl = getApplicationLogo();

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
			error = err instanceof Error ? err.message : m.auth_unexpected_error();
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
			error = err instanceof Error ? err.message : m.auth_passkey_failed();
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

<div class="relative z-[var(--arcane-z-raised)] flex min-h-dvh justify-center lg:px-8">
	<div class="grid min-h-dvh w-full max-w-screen-2xl grid-cols-1 lg:grid-cols-[1.05fr_minmax(420px,0.95fr)]">
		<aside class="showcase relative hidden flex-col justify-between overflow-hidden p-10 lg:flex xl:p-14">
			<div class="relative z-[var(--arcane-z-raised)] flex items-center gap-3">
				<div class="inline-flex size-10 items-center justify-center rounded-xl border bg-card/40 ring-1 ring-border/40">
					<img class="h-6 w-auto" src={logoUrl} alt="" />
				</div>
				<div class="flex flex-col leading-tight">
					<span class="text-sm font-medium tracking-wide text-foreground/90">{m.layout_title()}</span>
					{#if data.versionInformation?.displayVersion}
						<span class="font-mono text-[10px] tracking-wider text-muted-foreground/60"
							>{data.versionInformation.displayVersion}</span
						>
					{/if}
				</div>
			</div>

			<div class="relative z-[var(--arcane-z-raised)] max-w-xl">
				<h2 class="text-5xl leading-[1.05] font-semibold tracking-tight text-balance text-foreground xl:text-6xl">
					{m.auth_tagline_line1()}
					<span
						class="bg-gradient-to-br from-[var(--primary)] via-[var(--primary)] to-foreground/70 bg-clip-text text-transparent"
						>{m.auth_tagline_line2()}</span
					>
				</h2>
			</div>

			<div class="relative z-[var(--arcane-z-raised)] h-8"></div>
		</aside>

		<section class="form-pane relative flex min-h-dvh flex-col items-center justify-center p-6 sm:p-10 lg:p-10 xl:p-14">
			<div class="mb-8 flex w-full max-w-md justify-center lg:hidden">
				<div
					class="flex items-center justify-center rounded-2xl border bg-card/80 p-5 shadow-[0_8px_32px_-8px_rgba(0,0,0,0.35)] ring-1 ring-border/40"
				>
					<img class="h-16 w-auto" src={logoUrl} alt={m.layout_title()} />
				</div>
			</div>

			<div class="login-card-wrap relative w-full sm:w-[26rem] sm:max-w-full">
				<div class="mb-8 h-px w-10 bg-primary/70 shadow-[0_0_8px_var(--primary)]"></div>

				<div class="mb-8 flex flex-col text-left">
					<h1 class="text-3xl font-semibold tracking-tight sm:text-[2rem]">{m.welcome_back()}</h1>
					<p class="mt-2 text-sm text-muted-foreground">{m.auth_login_subtitle()}</p>
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
							<form
								id="login-form"
								name="login"
								action=""
								method="post"
								onsubmit={handleLogin}
								class="space-y-4"
								autocomplete="on"
							>
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

				<div class="mt-8 flex items-center justify-between gap-4 lg:hidden">
					<a
						href="https://github.com/ofkm/arcane"
						target="_blank"
						rel="noopener noreferrer"
						class="inline-flex items-center gap-1.5 rounded-full border bg-card/50 px-3 py-1.5 text-xs text-muted-foreground shadow-sm ring-1 ring-border/40 transition-colors hover:bg-card/70 hover:text-foreground"
					>
						<GithubIcon class="size-3.5" />
						{m.common_view_on_github()}
					</a>
					{#if data.versionInformation?.displayVersion}
						<span class="font-mono text-[11px] tracking-wider text-muted-foreground/60"
							>{data.versionInformation.displayVersion}</span
						>
					{/if}
				</div>
			</div>
		</section>
	</div>

	<div class="pointer-events-none absolute right-0 bottom-10 left-0 z-[var(--arcane-z-sticky)] hidden justify-center lg:flex">
		<a
			href="https://github.com/ofkm/arcane"
			target="_blank"
			rel="noopener noreferrer"
			class="pointer-events-auto inline-flex items-center gap-1.5 rounded-full border bg-card/50 px-3 py-1.5 text-xs text-muted-foreground shadow-sm ring-1 ring-border/40 transition-colors hover:bg-card/70 hover:text-foreground"
		>
			<GithubIcon class="size-3.5" />
			{m.common_view_on_github()}
		</a>
	</div>
</div>

<style>
	.showcase {
		position: relative;
		isolation: isolate;
	}

	.form-pane {
		isolation: isolate;
		contain: layout paint;
	}
</style>
