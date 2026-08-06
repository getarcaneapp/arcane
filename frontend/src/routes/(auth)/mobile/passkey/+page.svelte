<script lang="ts">
	import {
		browserSupportsWebAuthn,
		startAuthentication,
		startRegistration,
		type PublicKeyCredentialCreationOptionsJSON,
		type PublicKeyCredentialRequestOptionsJSON
	} from '@simplewebauthn/browser';
	import { onMount, tick } from 'svelte';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import AuthAmbient from '#lib/components/auth/auth-ambient.svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { AlertIcon, ApiKeyIcon, ShieldCheckIcon, SuccessIcon } from '#lib/icons';
	import { m } from '#lib/paraglide/messages';
	import { getApplicationLogo } from '#lib/utils/docker';
	import {
		MobilePasskeyBridgeRequestError,
		classifyMobilePasskeyError,
		decodeMobilePasskeyBridgeRequest,
		makeMobilePasskeyErrorCallback,
		makeMobilePasskeySuccessCallback,
		type MobilePasskeyBridgeRequest,
		type MobilePasskeyCredential
	} from './passkey-bridge';

	let {}: PageProps = $props();

	type CeremonyStatus = 'preparing' | 'ready' | 'working' | 'returning' | 'error';
	type PageError = 'invalid_request' | 'callback_failed';

	const logoUrl = getApplicationLogo();
	let status = $state<CeremonyStatus>('preparing');
	let pageError = $state<PageError>('invalid_request');
	let request = $state<MobilePasskeyBridgeRequest | null>(null);
	let serverOrigin = $state('');
	let callbackTimer: number | undefined;

	onMount(() => {
		void initializeCeremony();
		return () => {
			if (callbackTimer !== undefined) window.clearTimeout(callbackTimer);
		};
	});

	async function initializeCeremony() {
		serverOrigin = window.location.host;
		const fragment = window.location.hash;
		window.history.replaceState(null, '', `${window.location.pathname}${window.location.search}`);

		try {
			request = decodeMobilePasskeyBridgeRequest(fragment);
		} catch (error) {
			if (error instanceof MobilePasskeyBridgeRequestError && error.state) {
				returnToArcaneMobile(makeMobilePasskeyErrorCallback(error.state, error.code));
				return;
			}
			pageError = 'invalid_request';
			status = 'error';
			return;
		}

		if (!window.isSecureContext || !browserSupportsWebAuthn()) {
			returnToArcaneMobile(makeMobilePasskeyErrorCallback(request.state, 'unsupported'));
			return;
		}

		status = 'ready';
	}

	async function beginCeremony() {
		if (!request || status === 'working' || status === 'returning') return;
		status = 'working';
		await tick();

		try {
			let credential: MobilePasskeyCredential;
			if (request.operation === 'authenticate') {
				credential = await startAuthentication({
					optionsJSON: request.options as unknown as PublicKeyCredentialRequestOptionsJSON
				});
			} else {
				credential = await startRegistration({
					optionsJSON: request.options as unknown as PublicKeyCredentialCreationOptionsJSON
				});
			}

			returnToArcaneMobile(makeMobilePasskeySuccessCallback(request.state, credential));
		} catch (error) {
			returnToArcaneMobile(makeMobilePasskeyErrorCallback(request.state, classifyMobilePasskeyError(error)));
		}
	}

	function cancelCeremony() {
		if (!request) return;
		returnToArcaneMobile(makeMobilePasskeyErrorCallback(request.state, 'cancelled'));
	}

	function returnToArcaneMobile(callbackURL: string) {
		status = 'returning';
		window.location.assign(callbackURL);
		callbackTimer = window.setTimeout(() => {
			if (document.visibilityState === 'visible') {
				pageError = 'callback_failed';
				status = 'error';
			}
		}, 1800);
	}
</script>

<svelte:head>
	<title>{m.mobile_passkey_page_title()} | {m.layout_title()}</title>
	<meta name="robots" content="noindex,nofollow" />
</svelte:head>

<AuthAmbient />

<main class="relative z-[var(--arcane-z-raised)] flex min-h-dvh items-center justify-center p-5 sm:p-8">
	<section
		aria-labelledby="mobile-passkey-title"
		class="w-full max-w-md rounded-3xl border bg-card/75 p-6 shadow-[0_24px_80px_-32px_rgba(0,0,0,0.55)] ring-1 ring-border/40 backdrop-blur-xl sm:p-8"
	>
		<div class="mb-7 flex items-center justify-between gap-4">
			<div class="flex items-center gap-3">
				<div class="flex size-11 items-center justify-center rounded-xl border bg-background/50 ring-1 ring-border/40">
					<img class="h-7 w-auto" src={logoUrl} alt={m.layout_title()} />
				</div>
				<div class="leading-tight">
					<p class="text-sm font-semibold tracking-wide">{m.layout_title()}</p>
					<p class="mt-1 text-[11px] font-medium tracking-[0.12em] text-muted-foreground uppercase">
						{m.mobile_passkey_secure_handoff()}
					</p>
				</div>
			</div>
			<ShieldCheckIcon class="size-5 text-primary" />
		</div>

		<div class="mb-7 h-px w-10 bg-primary/70 shadow-[0_0_8px_var(--primary)]"></div>

		{#if status === 'preparing'}
			<div class="flex min-h-64 flex-col items-center justify-center text-center">
				<Spinner class="size-10 text-primary" />
				<h1 id="mobile-passkey-title" class="mt-5 text-2xl font-semibold tracking-tight">
					{m.mobile_passkey_preparing_title()}
				</h1>
				<p class="mt-2 text-sm text-muted-foreground">{m.mobile_passkey_preparing_description()}</p>
			</div>
		{:else if status === 'returning'}
			<div class="flex min-h-64 flex-col items-center justify-center text-center">
				<div class="flex size-14 items-center justify-center rounded-2xl border border-primary/25 bg-primary/10 text-primary">
					<SuccessIcon class="size-7" />
				</div>
				<h1 id="mobile-passkey-title" class="mt-5 text-2xl font-semibold tracking-tight">
					{m.mobile_passkey_returning_title()}
				</h1>
				<p class="mt-2 text-sm text-muted-foreground">{m.mobile_passkey_returning_description()}</p>
			</div>
		{:else if status === 'error'}
			<div class="space-y-5">
				<div>
					<h1 id="mobile-passkey-title" class="text-2xl font-semibold tracking-tight">
						{m.mobile_passkey_error_title()}
					</h1>
					<p class="mt-2 text-sm text-muted-foreground">{m.mobile_passkey_error_description()}</p>
				</div>
				<Alert.Root variant="destructive">
					<AlertIcon class="size-4" />
					<Alert.Title>{m.mobile_passkey_unable_to_continue()}</Alert.Title>
					<Alert.Description>
						{pageError === 'callback_failed' ? m.mobile_passkey_callback_failed() : m.mobile_passkey_invalid_request()}
					</Alert.Description>
				</Alert.Root>
			</div>
		{:else}
			<div>
				<div class="flex size-14 items-center justify-center rounded-2xl border border-primary/25 bg-primary/10 text-primary">
					<ApiKeyIcon class="size-7" />
				</div>
				<h1 id="mobile-passkey-title" class="mt-5 text-3xl font-semibold tracking-tight text-balance">
					{m.mobile_passkey_title()}
				</h1>
				<p class="mt-2 text-sm leading-relaxed text-muted-foreground">{m.mobile_passkey_description()}</p>
			</div>

			<div class="my-6 overflow-hidden rounded-2xl border bg-background/45 ring-1 ring-border/30">
				<div class="h-px w-full bg-gradient-to-r from-transparent via-primary/60 to-transparent"></div>
				<div class="flex items-center gap-3 p-4">
					<div class="flex size-9 shrink-0 items-center justify-center rounded-xl border bg-card text-primary">
						<ShieldCheckIcon class="size-4" />
					</div>
					<div class="min-w-0 flex-1">
						<p class="text-[11px] font-medium tracking-[0.1em] text-muted-foreground uppercase">
							{m.mobile_passkey_arcane_server()}
						</p>
						<p class="mt-1 truncate font-mono text-sm font-medium" title={serverOrigin}>{serverOrigin}</p>
					</div>
					<span class="relative flex size-2.5" aria-hidden="true">
						<span class="absolute inline-flex size-full animate-ping rounded-full bg-primary opacity-40"></span>
						<span class="relative inline-flex size-2.5 rounded-full bg-primary"></span>
					</span>
				</div>
			</div>

			<div class="space-y-2">
				<ArcaneButton
					action="login"
					icon={ApiKeyIcon}
					customLabel={m.mobile_passkey_continue()}
					loadingLabel={m.mobile_passkey_waiting()}
					hoverEffect="none"
					loading={status === 'working'}
					disabled={status === 'working'}
					onclick={() => void beginCeremony()}
				/>
				<ArcaneButton
					action="base"
					tone="ghost"
					customLabel={m.common_cancel()}
					hoverEffect="none"
					class="w-full"
					disabled={status === 'working'}
					onclick={cancelCeremony}
				/>
			</div>

			<p class="mt-5 text-center text-xs leading-relaxed text-muted-foreground">
				{m.mobile_passkey_private_key_notice()}
			</p>
		{/if}
	</section>
</main>

<style>
	@media (prefers-reduced-motion: reduce) {
		:global(.animate-ping) {
			animation: none;
		}
	}
</style>
