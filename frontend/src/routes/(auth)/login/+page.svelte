<script lang="ts">
	import { Label } from '$lib/components/ui/label/index.js';
	import * as Card from '$lib/components/ui/card/index.js';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import * as InputGroup from '$lib/components/ui/input-group/index.js';
	import { AlertIcon, LockIcon, UserIcon, GithubIcon, OpenIdIcon } from '$lib/icons';
	import type { PageData } from './$types';
	import { goto } from '$app/navigation';
	import userStore from '$lib/stores/user-store';
	import { m } from '$lib/paraglide/messages';
	import { authService } from '$lib/services/auth-service';
	import { getApplicationLogo } from '$lib/utils/image.util';
	import { Motion } from 'svelte-motion';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { onMount } from 'svelte';

	let { data }: { data: PageData } = $props();

	let isLoading = $state({
		local: false,
		oidc: false
	});
	let error = $state<string | null>(null);
	let username = $state('');
	let password = $state('');

	let logoUrl = $derived(getApplicationLogo());

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

	onMount(() => {
		if (oidcAutoRedirect && oidcEnabledBySettings && !data.error) {
			handleOidcLogin();
		}
	});

	async function handleOidcLogin() {
		isLoading.oidc = true;
		const currentRedirect = data.redirectTo || '/dashboard';
		await goto(`/oidc/login?redirect=${encodeURIComponent(currentRedirect)}`);
		isLoading.oidc = false;
	}

	async function handleLogin(event: Event) {
		event.preventDefault();

		if (!username || !password) {
			error = 'Please enter both username and password';
			return;
		}

		isLoading.local = true;
		error = null;

		try {
			const user = await authService.login({ username, password });
			userStore.setUser(user);
			const redirectTo = data.redirectTo || '/dashboard';
			await goto(redirectTo, { replaceState: true });
		} catch (err) {
			error = err instanceof Error ? err.message : 'Login failed';
		} finally {
			isLoading.local = false;
		}
	}

	const showDivider = $derived(showOidcLoginButton && showLocalLoginForm);

	function getOrbAnimation(startX: number, startY: number, duration: number, delay: number) {
		const xOffset = startX * 4;
		const yOffset = startY * 4;

		return {
			animate: {
				x: [-20 + xOffset, 60 + xOffset, 20 + xOffset, -80 + xOffset, -20 + xOffset],
				y: [-20 + yOffset, -80 + yOffset, 20 + yOffset, 60 + yOffset, -20 + yOffset],
				scale: [1, 1.15, 0.95, 1.1, 1]
			},
			transition: {
				duration: duration,
				ease: 'easeInOut',
				repeat: Infinity,
				delay: delay
			}
		};
	}

	const orb1Anim = getOrbAnimation(-20, 30, 18, 0.5);
	const orb2Anim = getOrbAnimation(35, -10, 22, 1.2);
	const orb3Anim = getOrbAnimation(-15, -30, 20, 0.8);
	const orb4Anim = getOrbAnimation(25, 15, 16, 1.8);
</script>

<div class="fixed inset-0 overflow-hidden">
	<Motion animate={orb1Anim.animate} transition={orb1Anim.transition} let:motion>
		<div
			use:motion
			class="bg-primary absolute top-[-150px] left-[10%] h-[330px] w-[330px] rounded-full opacity-30 blur-[57px] md:h-[500px] md:w-[500px] md:blur-[85px]"
		></div>
	</Motion>
	<Motion animate={orb2Anim.animate} transition={orb2Anim.transition} let:motion>
		<div
			use:motion
			class="bg-primary absolute right-[15%] bottom-[-150px] h-[280px] w-[280px] rounded-full opacity-30 blur-[57px] md:h-[420px] md:w-[420px] md:blur-[85px]"
		></div>
	</Motion>
	<Motion animate={orb3Anim.animate} transition={orb3Anim.transition} let:motion>
		<div
			use:motion
			class="bg-primary absolute top-[20%] right-[-120px] h-[250px] w-[250px] rounded-full opacity-30 blur-[57px] md:h-[380px] md:w-[380px] md:blur-[85px]"
		></div>
	</Motion>
	<Motion animate={orb4Anim.animate} transition={orb4Anim.transition} let:motion>
		<div
			use:motion
			class="bg-primary absolute bottom-[30%] left-[-100px] h-[210px] w-[210px] rounded-full opacity-30 blur-[57px] md:h-[320px] md:w-[320px] md:blur-[85px]"
		></div>
	</Motion>
</div>

<div class="relative flex min-h-dvh flex-col items-center p-6 md:p-10">
	<div class="flex w-full flex-1 flex-col items-center justify-center">
		<div class="w-full max-w-md">
			<div class="mb-8 flex justify-center">
				<div class="bg-card/60 flex items-center justify-center rounded-2xl border p-6 shadow-lg backdrop-blur-2xl">
					<img class="h-24 w-auto" src={logoUrl} alt={m.layout_title()} />
				</div>
			</div>

			<Card.Root class="bg-card/60 flex flex-col gap-6 overflow-hidden border shadow-lg backdrop-blur-2xl">
				<Card.Content class="p-8">
					<div class="mb-8 flex flex-col items-center text-center">
						<h1 class="text-3xl font-bold tracking-tight">{m.auth_welcome_back_title()}</h1>
						<p class="text-muted-foreground mt-2 text-sm text-balance">{m.auth_login_subtitle()}</p>
					</div>

					<div class="space-y-4">
						{#if data.error}
							<Alert.Root variant="destructive" class="bg-card/60 border backdrop-blur-2xl">
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
							<Alert.Root variant="destructive" class="bg-card/60 border backdrop-blur-2xl">
								<AlertIcon class="size-4" />
								<Alert.Title>{m.auth_login_problem_title()}</Alert.Title>
								<Alert.Description>{data.errorMessage}</Alert.Description>
							</Alert.Root>
						{/if}

						{#if error}
							<Alert.Root variant="destructive" class="bg-card/60 border backdrop-blur-2xl">
								<AlertIcon class="size-4" />
								<Alert.Title>{m.auth_failed_title()}</Alert.Title>
								<Alert.Description>{error}</Alert.Description>
							</Alert.Root>
						{/if}

						{#if !showLocalLoginForm && !showOidcLoginButton}
							<Alert.Root variant="destructive" class="bg-card/60 border backdrop-blur-2xl">
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
								loading={isLoading.oidc}
								disabled={isLoading.local}
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
											disabled={isLoading.local || isLoading.oidc}
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
											disabled={isLoading.local || isLoading.oidc}
										/>
									</InputGroup.Root>
								</div>
								<ArcaneButton
									type="submit"
									action="login"
									loading={isLoading.local}
									disabled={isLoading.oidc}
									hoverEffect="none"
								/>
							</form>

							{#if showDivider}
								<div class="relative my-4">
									<div class="absolute inset-0 flex items-center">
										<div class="border-border/60 w-full border-t"></div>
									</div>
									<div class="relative flex justify-center text-xs">
										<span class="bg-card/60 text-muted-foreground rounded-full border px-3 py-1 backdrop-blur-2xl">
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
									loading={isLoading.oidc}
									disabled={isLoading.local}
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
				</Card.Content>
			</Card.Root>
		</div>
	</div>

	<div class="mt-auto pt-8 pb-4">
		<div class="text-muted-foreground flex flex-col items-center justify-center gap-2">
			<a
				href="https://github.com/ofkm/arcane"
				target="_blank"
				rel="noopener noreferrer"
				class="bg-card/60 hover:text-primary inline-flex items-center gap-1.5 rounded-full border px-3 py-1.5 text-xs shadow-sm backdrop-blur-2xl transition-colors"
			>
				<GithubIcon class="size-4" />
				{m.common_view_on_github()}
			</a>
			{#if data.versionInformation?.displayVersion}
				<span class="text-xs opacity-50">{data.versionInformation.displayVersion}</span>
			{/if}
		</div>
	</div>
</div>
