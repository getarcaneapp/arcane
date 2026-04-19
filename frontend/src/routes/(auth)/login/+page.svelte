<script lang="ts">
	import { Label } from '$lib/components/ui/label/index.js';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import * as InputGroup from '$lib/components/ui/input-group/index.js';
	import { AlertIcon, LockIcon, UserIcon, GithubIcon, OpenIdIcon } from '$lib/icons';
	import type { PageData } from './$types';
	import { goto } from '$app/navigation';
	import userStore from '$lib/stores/user-store';
	import { m } from '$lib/paraglide/messages';
	import { authService } from '$lib/services/auth-service';
	import { queryKeys } from '$lib/query/query-keys';
	import { getApplicationLogo } from '$lib/utils/image.util';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { onMount } from 'svelte';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';

	let { data }: { data: PageData } = $props();

	let error = $state<string | null>(null);
	let username = $state('');
	let password = $state('');
	const queryClient = useQueryClient();

	const accentColor = $derived(data.settings?.accentColor);
	const logoUrl = $derived(getApplicationLogo(false, accentColor, accentColor));

	const oidcEnabledBySettings = $derived(data.settings?.oidcEnabled === true);
	const showOidcLoginButton = $derived(oidcEnabledBySettings);

	const localAuthEnabledBySettings = $derived(data.settings?.authLocalEnabled !== false);
	const showLocalLoginForm = $derived(localAuthEnabledBySettings);

	const oidcAutoRedirect = $derived(data.settings?.oidcAutoRedirectToProvider === true);

	const oidcProviderName = $derived(data.settings?.oidcProviderName || '');
	const oidcProviderLogoUrl = $derived(data.settings?.oidcProviderLogoUrl || '');
	const oidcButtonLabel = $derived(
		oidcProviderName ? m.auth_oidc_signin_with({ provider: oidcProviderName }) : m.auth_oidc_signin()
	);

	const oidcLoginMutation = createMutation(() => ({
		mutationFn: async () => {
			error = null;
			const currentRedirect = data.redirectTo || '/dashboard';
			await goto(`/oidc/login?redirect=${encodeURIComponent(currentRedirect)}`);
		}
	}));

	const loginMutation = createMutation(() => ({
		mutationFn: () => authService.login({ username, password }),
		onSuccess: async (user) => {
			userStore.setUser(user);
			await queryClient.invalidateQueries({ queryKey: queryKeys.auth.all });
			const redirectTo = data.redirectTo || '/dashboard';
			await goto(redirectTo, { replaceState: true });
		},
		onError: (err) => {
			error = err instanceof Error ? err.message : 'Login failed';
		}
	}));

	const isLocalLoading = $derived(loginMutation.isPending);
	const isOidcLoading = $derived(oidcLoginMutation.isPending);

	onMount(() => {
		if (oidcAutoRedirect && oidcEnabledBySettings && !data.error) {
			oidcLoginMutation.mutate();
		}
	});

	function handleOidcLogin() {
		const currentRedirect = data.redirectTo || '/dashboard';
		oidcLoginMutation.mutate(undefined, {
			onError: () => {
				// Fallback to direct navigation when mutation fails unexpectedly
				void goto(`/oidc/login?redirect=${encodeURIComponent(currentRedirect)}`);
			}
		});
	}

	function handleLogin(event: Event) {
		event.preventDefault();

		if (!username || !password) {
			error = 'Please enter both username and password';
			return;
		}

		error = null;
		loginMutation.mutate();
	}

	const showDivider = $derived(showOidcLoginButton && showLocalLoginForm);
</script>

<svelte:head>
	<title>{m.layout_title()}</title>
</svelte:head>

<div class="ambient" aria-hidden="true">
	<div class="ambient__mesh"></div>
	<div class="ambient__shimmer"></div>
	<div class="ambient__grid"></div>
	<div class="ambient__noise"></div>
	<div class="ambient__vignette"></div>
</div>

<div class="relative z-10 grid min-h-dvh grid-cols-1 lg:grid-cols-[1.05fr_minmax(420px,0.95fr)]">
	<aside class="showcase relative hidden flex-col justify-between overflow-hidden p-10 lg:flex xl:p-14">
		<div class="relative z-10 flex items-center gap-3">
			<div class="bg-card/40 ring-border/40 inline-flex size-10 items-center justify-center rounded-xl border ring-1">
				<img class="h-6 w-auto" src={logoUrl} alt="" />
			</div>
			<div class="flex flex-col leading-tight">
				<span class="text-foreground/90 text-sm font-medium tracking-wide">{m.layout_title()}</span>
				{#if data.versionInformation?.displayVersion}
					<span class="text-muted-foreground/60 font-mono text-[10px] tracking-wider"
						>{data.versionInformation.displayVersion}</span
					>
				{/if}
			</div>
		</div>

		<div class="relative z-10 max-w-xl">
			<h2 class="text-foreground text-5xl leading-[1.05] font-semibold tracking-tight text-balance xl:text-6xl">
				{m.auth_tagline_line1()}
				<span class="to-foreground/70 bg-gradient-to-br from-[var(--primary)] via-[var(--primary)] bg-clip-text text-transparent"
					>{m.auth_tagline_line2()}</span
				>
			</h2>
		</div>

		<div class="relative z-10 flex items-end justify-between gap-6">
			<a
				href="https://github.com/ofkm/arcane"
				target="_blank"
				rel="noopener noreferrer"
				class="bg-card/50 ring-border/40 text-muted-foreground hover:text-foreground hover:bg-card/70 inline-flex items-center gap-1.5 rounded-full border px-3 py-1.5 text-xs shadow-sm ring-1 transition-colors"
			>
				<GithubIcon class="size-3.5" />
				{m.common_view_on_github()}
			</a>
		</div>
	</aside>

	<section class="form-pane relative flex min-h-dvh flex-col items-center justify-center p-6 sm:p-10 lg:p-10 xl:p-14">
		<div class="mb-8 flex w-full max-w-md justify-center lg:hidden">
			<div
				class="bg-card/80 ring-border/40 flex items-center justify-center rounded-2xl border p-5 shadow-[0_8px_32px_-8px_rgba(0,0,0,0.35)] ring-1"
			>
				<img class="h-16 w-auto" src={logoUrl} alt={m.layout_title()} />
			</div>
		</div>

		<div class="login-card-wrap relative w-full sm:w-[26rem] sm:max-w-full">
			<div class="bg-primary/70 mb-8 h-px w-10 shadow-[0_0_8px_var(--primary)]"></div>

			<div class="mb-8 flex flex-col text-left">
				<h1 class="text-3xl font-semibold tracking-tight sm:text-[2rem]">{m.auth_welcome_back_title()}</h1>
				<p class="text-muted-foreground mt-2 text-sm">{m.auth_login_subtitle()}</p>
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

				{#if !showLocalLoginForm && !showOidcLoginButton}
					<Alert.Root variant="destructive">
						<AlertIcon class="size-4" />
						<Alert.Title>{m.auth_no_login_methods_title()}</Alert.Title>
						<Alert.Description>{m.auth_no_login_methods_description()}</Alert.Description>
					</Alert.Root>
				{/if}

				{#if showOidcLoginButton && !showLocalLoginForm}
					<ArcaneButton
						hoverEffect="none"
						action="oidc_login"
						onclick={() => handleOidcLogin()}
						loading={isOidcLoading}
						disabled={isLocalLoading}
						icon={null}
						customLabel=""
					>
						{#if oidcProviderLogoUrl}
							<img src={oidcProviderLogoUrl} alt="" class="size-4 object-contain" />
						{:else}
							<OpenIdIcon class="size-4" />
						{/if}
						{oidcButtonLabel}
					</ArcaneButton>
				{/if}

				{#if showLocalLoginForm}
					<form onsubmit={handleLogin} class="space-y-4">
						<div class="space-y-2">
							<Label for="username" class="text-xs">{m.common_username()}</Label>
							<InputGroup.Root>
								<InputGroup.Addon>
									<UserIcon />
								</InputGroup.Addon>
								<InputGroup.Input
									id="username"
									name="username"
									type="text"
									autocomplete="username"
									required
									bind:value={username}
									placeholder={m.auth_username_placeholder()}
									disabled={isLocalLoading || isOidcLoading}
								/>
							</InputGroup.Root>
						</div>
						<div class="space-y-2">
							<Label for="password" class="text-xs">{m.common_password()}</Label>
							<InputGroup.Root>
								<InputGroup.Addon>
									<LockIcon />
								</InputGroup.Addon>
								<InputGroup.Input
									id="password"
									name="password"
									type="password"
									autocomplete="current-password"
									required
									bind:value={password}
									placeholder={m.auth_password_placeholder()}
									disabled={isLocalLoading || isOidcLoading}
								/>
							</InputGroup.Root>
						</div>
						<ArcaneButton type="submit" action="login" loading={isLocalLoading} disabled={isOidcLoading} hoverEffect="none" />
					</form>

					{#if showDivider}
						<div class="relative my-4">
							<div class="absolute inset-0 flex items-center">
								<div class="border-border/60 w-full border-t"></div>
							</div>
							<div class="relative flex justify-center text-xs">
								<span
									class="bg-card/70 ring-border/40 text-muted-foreground rounded-full border px-3 py-1 shadow-sm ring-1 backdrop-blur-md"
								>
									{m.auth_or_continue()}
								</span>
							</div>
						</div>
					{/if}

					{#if showOidcLoginButton && showDivider}
						<ArcaneButton
							action="oidc_login"
							hoverEffect="none"
							onclick={() => handleOidcLogin()}
							loading={isOidcLoading}
							disabled={isLocalLoading}
							icon={null}
							customLabel=""
						>
							{#if oidcProviderLogoUrl}
								<img src={oidcProviderLogoUrl} alt="" class="size-4 object-contain" />
							{:else}
								<OpenIdIcon class="size-4" />
							{/if}
							{oidcButtonLabel}
						</ArcaneButton>
					{/if}
				{/if}
			</div>

			<div class="mt-8 flex items-center justify-between gap-4 lg:hidden">
				<a
					href="https://github.com/ofkm/arcane"
					target="_blank"
					rel="noopener noreferrer"
					class="bg-card/50 ring-border/40 text-muted-foreground hover:text-foreground hover:bg-card/70 inline-flex items-center gap-1.5 rounded-full border px-3 py-1.5 text-xs shadow-sm ring-1 transition-colors"
				>
					<GithubIcon class="size-3.5" />
					{m.common_view_on_github()}
				</a>
				{#if data.versionInformation?.displayVersion}
					<span class="text-muted-foreground/60 font-mono text-[11px] tracking-wider"
						>{data.versionInformation.displayVersion}</span
					>
				{/if}
			</div>
		</div>
	</section>
</div>

<style>
	.ambient {
		position: fixed;
		inset: 0;
		z-index: 0;
		overflow: hidden;
		pointer-events: none;
		background: var(--background);
		contain: strict;
	}

	.ambient__mesh {
		position: absolute;
		inset: -20%;
		background:
			radial-gradient(ellipse 60% 50% at 18% 22%, color-mix(in oklab, var(--primary) 14%, transparent) 0%, transparent 60%),
			radial-gradient(ellipse 55% 45% at 82% 78%, color-mix(in oklab, var(--primary) 10%, transparent) 0%, transparent 65%),
			radial-gradient(ellipse 45% 55% at 78% 18%, color-mix(in oklab, var(--primary) 8%, transparent) 0%, transparent 60%),
			radial-gradient(ellipse 50% 40% at 22% 82%, color-mix(in oklab, var(--primary) 6%, transparent) 0%, transparent 65%);
		background-repeat: no-repeat;
		filter: saturate(1);
		opacity: 0.5;
	}

	.ambient__shimmer {
		position: absolute;
		top: -400px;
		left: -400px;
		width: 3000px;
		height: 3000px;
		background: conic-gradient(
			from 0deg,
			transparent 0deg,
			color-mix(in oklab, var(--primary) 8%, transparent) 60deg,
			transparent 120deg,
			color-mix(in oklab, var(--primary) 5%, transparent) 200deg,
			transparent 280deg,
			color-mix(in oklab, var(--primary) 7%, transparent) 340deg,
			transparent 360deg
		);
		opacity: 0.4;
		will-change: transform;
		transform: translateZ(0) rotate(0deg);
		transform-origin: center center;
		animation: shimmerRotate 60s linear infinite;
	}

	@keyframes shimmerRotate {
		from {
			transform: translateZ(0) rotate(0deg);
		}
		to {
			transform: translateZ(0) rotate(360deg);
		}
	}

	.ambient__grid {
		position: absolute;
		inset: 0;
		background-image: url("data:image/svg+xml,%3Csvg width='48' height='48' viewBox='0 0 48 48' xmlns='http://www.w3.org/2000/svg'%3E%3Cpath d='M0 0h48v48H0z' fill='none'/%3E%3Cpath d='M47.5 0v48M0 47.5h48' stroke='rgba(150, 150, 150, 1)' stroke-width='1' stroke-opacity='0.15'/%3E%3C/svg%3E");
		background-repeat: repeat;
		background-size: 48px 48px;
		mask-image: radial-gradient(circle at 50% 50%, #000 30%, transparent 80%);
		-webkit-mask-image: radial-gradient(circle at 50% 50%, #000 30%, transparent 80%);
		opacity: 0.5;
	}

	.ambient__noise {
		position: absolute;
		inset: 0;
		opacity: 0.05;
		background-image: url("data:image/svg+xml;utf8,<svg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'><filter id='n'><feTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='2' stitchTiles='stitch'/><feColorMatrix values='0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 1 0'/></filter><rect width='100%' height='100%' filter='url(%23n)'/></svg>");
	}

	.ambient__vignette {
		position: absolute;
		inset: 0;
		background: radial-gradient(ellipse at center, transparent 40%, color-mix(in oklab, var(--background) 80%, transparent) 100%);
	}

	.showcase {
		position: relative;
		isolation: isolate;
	}

	.form-pane {
		isolation: isolate;
		contain: layout paint;
	}

	@media (prefers-reduced-motion: reduce) {
		.ambient__shimmer {
			animation: none;
		}
	}
</style>
