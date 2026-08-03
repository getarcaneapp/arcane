<script lang="ts">
	import { startAuthentication, type PublicKeyCredentialRequestOptionsJSON } from '@simplewebauthn/browser';
	import { ApiKeyIcon, AlertIcon } from '#lib/icons';
	import { m } from '#lib/paraglide/messages';
	import { passkeyService } from '#lib/services/passkey-service';
	import type { AuthenticationResponse, MFAChallenge } from '#lib/types/auth';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { Button } from '#lib/components/ui/button';
	import * as InputGroup from '#lib/components/ui/input-group/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';

	let {
		challenge,
		onComplete,
		onCancel
	}: {
		challenge: MFAChallenge;
		onComplete: (response: AuthenticationResponse) => Promise<void>;
		onCancel?: () => void;
	} = $props();

	let recoveryCode = $state('');
	let busy = $state(false);
	let error = $state<string | null>(null);

	function isCancelledError(value: unknown): boolean {
		return value instanceof Error && value.name === 'NotAllowedError';
	}

	async function completeWithPasskey() {
		busy = true;
		error = null;
		try {
			const credential = await startAuthentication({
				optionsJSON: challenge.options as unknown as PublicKeyCredentialRequestOptionsJSON
			});
			const response = await passkeyService.finishMFA(challenge.transactionId, credential);
			await onComplete(response);
		} catch (value) {
			if (!isCancelledError(value)) {
				error = value instanceof Error ? value.message : m.auth_mfa_failed();
			}
		} finally {
			busy = false;
		}
	}

	async function completeWithRecoveryCode(event: SubmitEvent) {
		event.preventDefault();
		if (!recoveryCode.trim()) return;

		busy = true;
		error = null;
		try {
			const response = await passkeyService.finishRecovery(challenge.transactionId, recoveryCode.trim());
			await onComplete(response);
		} catch (value) {
			error = value instanceof Error ? value.message : m.auth_mfa_failed();
		} finally {
			busy = false;
		}
	}
</script>

<div class="space-y-5 rounded-2xl border bg-card/60 p-5 shadow-sm ring-1 ring-border/40">
	<div class="space-y-2">
		<div class="flex items-center gap-2">
			<ApiKeyIcon class="size-5 text-primary" />
			<h2 class="text-lg font-semibold">{m.auth_mfa_title()}</h2>
		</div>
		<p class="text-sm text-muted-foreground">{m.auth_mfa_description()}</p>
	</div>

	{#if error}
		<Alert.Root variant="destructive">
			<AlertIcon class="size-4" />
			<Alert.Title>{m.auth_failed_title()}</Alert.Title>
			<Alert.Description>{error}</Alert.Description>
		</Alert.Root>
	{/if}

	<ArcaneButton
		action="login"
		icon={ApiKeyIcon}
		customLabel={m.auth_mfa_use_passkey()}
		hoverEffect="none"
		loading={busy}
		disabled={busy}
		onclick={() => void completeWithPasskey()}
	/>

	<div class="relative flex items-center">
		<div class="w-full border-t border-border/60"></div>
		<span class="absolute left-1/2 -translate-x-1/2 rounded-full border bg-card px-3 py-1 text-xs text-muted-foreground">
			{m.auth_or_continue()}
		</span>
	</div>

	<form class="space-y-3" onsubmit={completeWithRecoveryCode}>
		<Label for="mfa-recovery-code">{m.auth_mfa_recovery_code()}</Label>
		<InputGroup.Root role={undefined}>
			<InputGroup.Input
				id="mfa-recovery-code"
				name="recoveryCode"
				autocomplete="one-time-code"
				placeholder={m.auth_mfa_recovery_code_placeholder()}
				bind:value={recoveryCode}
				disabled={busy}
			/>
		</InputGroup.Root>
		<Button type="submit" variant="outline" class="w-full" disabled={busy || !recoveryCode.trim()}>
			{m.auth_mfa_use_recovery_code()}
		</Button>
	</form>

	{#if onCancel}
		<Button type="button" variant="ghost" class="w-full" disabled={busy} onclick={() => onCancel?.()}>
			{m.auth_mfa_cancel()}
		</Button>
	{/if}
</div>
