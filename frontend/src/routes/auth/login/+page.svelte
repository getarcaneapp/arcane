<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import * as Card from '$lib/components/ui/card/index.js';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import * as InputGroup from '$lib/components/ui/input-group/index.js';
	import CircleAlertIcon from '@lucide/svelte/icons/alert-circle';
	import LogInIcon from '@lucide/svelte/icons/log-in';
	import LockIcon from '@lucide/svelte/icons/lock';
	import UserIcon from '@lucide/svelte/icons/user';
	import type { PageData } from './$types';
	import { goto } from '$app/navigation';
	import userStore from '$lib/stores/user-store';
	import { m } from '$lib/paraglide/messages';
	import { authService } from '$lib/services/auth-service';
	import { getApplicationLogo } from '$lib/utils/image.util';
	import GithubIcon from '$lib/icons/github-icon.svelte';

	let { data }: { data: PageData } = $props();

	let loading = $state(false);
	let error = $state<string | null>(null);
	let username = $state('');
	let password = $state('');

	// Make logo URL reactive to accent color changes
	let logoUrl = $derived(getApplicationLogo());

	const oidcEnabledBySettings = $derived(data.settings?.oidcEnabled === true);
	const showOidcLoginButton = $derived(oidcEnabledBySettings);

	const localAuthEnabledBySettings = $derived(data.settings?.authLocalEnabled !== false);
	const showLocalLoginForm = $derived(localAuthEnabledBySettings);

	function handleOidcLogin() {
		const currentRedirect = data.redirectTo || '/dashboard';
		goto(`/auth/oidc/login?redirect=${encodeURIComponent(currentRedirect)}`);
	}

	async function handleLogin(event: Event) {
		event.preventDefault();

		if (!username || !password) {
			error = 'Please enter both username and password';
			return;
		}

		loading = true;
		error = null;

		try {
			const user = await authService.login({ username, password });
			userStore.setUser(user);
			const redirectTo = data.redirectTo || '/dashboard';
			goto(redirectTo, { replaceState: true });
		} catch (err) {
			error = err instanceof Error ? err.message : 'Login failed';
		} finally {
			loading = false;
		}
	}

	const showDivider = $derived(showOidcLoginButton && showLocalLoginForm);

	// Generate random starting positions for each orb
	const orb1X = Math.random() * 80 - 40;
	const orb1Y = Math.random() * 80 - 40;
	const orb2X = Math.random() * 80 - 40;
	const orb2Y = Math.random() * 80 - 40;
	const orb3X = Math.random() * 80 - 40;
	const orb3Y = Math.random() * 80 - 40;
	const orb4X = Math.random() * 80 - 40;
	const orb4Y = Math.random() * 80 - 40;

	// Add random delay for each orb to create natural animation
	const orb1Delay = Math.random() * 2;
	const orb2Delay = Math.random() * 2;
	const orb3Delay = Math.random() * 2;
	const orb4Delay = Math.random() * 2;
</script>

<div class="fixed inset-0 overflow-hidden">
	<div
		class="bg-primary orb absolute top-[-150px] left-[10%] h-[330px] w-[330px] rounded-full opacity-30 blur-[57px] md:h-[500px] md:w-[500px] md:blur-[85px]"
		style="--start-x: {orb1X}; --start-y: {orb1Y}; --orb-delay: {orb1Delay}s; --orb-duration: 18s;"
	></div>
	<div
		class="bg-primary orb absolute right-[15%] bottom-[-150px] h-[280px] w-[280px] rounded-full opacity-30 blur-[57px] md:h-[420px] md:w-[420px] md:blur-[85px]"
		style="--start-x: {orb2X}; --start-y: {orb2Y}; --orb-delay: {orb2Delay}s; --orb-duration: 22s;"
	></div>
	<div
		class="bg-primary orb absolute top-[20%] right-[-120px] h-[250px] w-[250px] rounded-full opacity-30 blur-[57px] md:h-[380px] md:w-[380px] md:blur-[85px]"
		style="--start-x: {orb3X}; --start-y: {orb3Y}; --orb-delay: {orb3Delay}s; --orb-duration: 20s;"
	></div>
	<div
		class="bg-primary orb absolute bottom-[30%] left-[-100px] h-[210px] w-[210px] rounded-full opacity-30 blur-[57px] md:h-[320px] md:w-[320px] md:blur-[85px]"
		style="--start-x: {orb4X}; --start-y: {orb4Y}; --orb-delay: {orb4Delay}s; --orb-duration: 16s;"
	></div>
</div>

<div class="relative flex min-h-screen flex-col items-center justify-center p-6 md:p-10">
	<div class="w-full max-w-md">
		<div class="mb-8 flex justify-center">
			<div class="glass-light bubble bubble-shadow-lg flex items-center justify-center rounded-2xl p-6">
				<img class="h-24 w-auto" src={logoUrl} alt={m.layout_title()} />
			</div>
		</div>

		<Card.Root class="bubble bubble-shadow-lg flex flex-col gap-6 overflow-hidden">
			<Card.Content class="p-8">
				<div class="mb-8 flex flex-col items-center text-center">
					<h1 class="text-3xl font-bold tracking-tight">{m.auth_welcome_back_title()}</h1>
					<p class="text-muted-foreground mt-2 text-sm text-balance">{m.auth_login_subtitle()}</p>
				</div>

				<div class="space-y-4">
					{#if data.error}
						<Alert.Root variant="destructive" class="glass-light">
							<CircleAlertIcon class="size-4" />
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
								{:else}
									{m.auth_unexpected_error()}
								{/if}
							</Alert.Description>
						</Alert.Root>
					{/if}

					{#if error}
						<Alert.Root variant="destructive" class="glass-light">
							<CircleAlertIcon class="size-4" />
							<Alert.Title>{m.auth_failed_title()}</Alert.Title>
							<Alert.Description>{error}</Alert.Description>
						</Alert.Root>
					{/if}

					{#if !showLocalLoginForm && !showOidcLoginButton}
						<Alert.Root variant="destructive" class="glass-light">
							<CircleAlertIcon class="size-4" />
							<Alert.Title>{m.auth_no_login_methods_title()}</Alert.Title>
							<Alert.Description>{m.auth_no_login_methods_description()}</Alert.Description>
						</Alert.Root>
					{/if}

					{#if showOidcLoginButton && !showLocalLoginForm}
						<Button onclick={handleOidcLogin} class="hover-lift w-full" size="lg">
							<LogInIcon class="mr-2 size-4" />
							{m.auth_oidc_signin()}
						</Button>
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
										disabled={loading}
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
										disabled={loading}
									/>
								</InputGroup.Root>
							</div>
							<Button type="submit" class="hover-lift w-full" size="lg" disabled={loading} aria-busy={loading}>
								{#if loading}
									<div class="mr-2 size-4 animate-spin rounded-full border-2 border-white/30 border-t-white"></div>
									{m.auth_signing_in()}
								{:else}
									<LogInIcon class="mr-2 size-4" />
									{m.auth_signin_button()}
								{/if}
							</Button>
						</form>

						{#if showDivider}
							<div class="relative my-4">
								<div class="absolute inset-0 flex items-center">
									<div class="border-border/60 w-full border-t"></div>
								</div>
								<div class="relative flex justify-center text-xs">
									<span class="glass-light bubble-pill text-muted-foreground px-3 py-1">
										{m.auth_or_continue()}
									</span>
								</div>
							</div>
						{/if}

						{#if showOidcLoginButton && showDivider}
							<Button onclick={handleOidcLogin} variant="outline" class="hover-lift w-full" size="lg">
								<LogInIcon class="mr-2 size-4" />
								{m.auth_oidc_signin()}
							</Button>
						{/if}
					{/if}
				</div>
			</Card.Content>
		</Card.Root>
	</div>
</div>

<div class="fixed right-0 bottom-4 left-0 p-4">
	<div class="text-muted-foreground flex flex-col items-center justify-center gap-2">
		<a
			href="https://github.com/ofkm/arcane"
			target="_blank"
			rel="noopener noreferrer"
			class="glass-light bubble-pill hover:text-primary flex items-center gap-1.5 text-xs transition-colors"
		>
			<GithubIcon class="size-4" />
			{m.common_view_on_github()}
		</a>
		{#if data.versionInformation?.displayVersion}
			<span class="text-xs opacity-50">{data.versionInformation.displayVersion}</span>
		{/if}
	</div>
</div>

<style>
	.orb {
		transform: translate(calc(-20px + var(--start-x) * 4px), calc(-20px + var(--start-y) * 4px)) scale(1);
		animation: orb-float var(--orb-duration, 18s) ease-in-out infinite;
		animation-delay: var(--orb-delay, 0s);
	}

	@keyframes orb-float {
		0%,
		100% {
			transform: translate(calc(-20px + var(--start-x) * 4px), calc(-20px + var(--start-y) * 4px)) scale(1);
		}
		25% {
			transform: translate(calc(60px + var(--start-x) * 4px), calc(-80px + var(--start-y) * 4px)) scale(1.15);
		}
		50% {
			transform: translate(calc(20px + var(--start-x) * 4px), calc(20px + var(--start-y) * 4px)) scale(0.95);
		}
		75% {
			transform: translate(calc(-80px + var(--start-x) * 4px), calc(60px + var(--start-y) * 4px)) scale(1.1);
		}
	}

	@media (prefers-reduced-motion: reduce) {
		.orb {
			animation: none;
		}
	}
</style>
