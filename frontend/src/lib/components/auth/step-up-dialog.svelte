<script lang="ts">
	import { startAuthentication } from '@simplewebauthn/browser';

	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as InputGroup from '#lib/components/ui/input-group/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { AlertIcon, ApiKeyIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { passkeyService } from '#lib/services/passkey-service.js';
	import type { StepUpGrant } from '#lib/types/auth.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let {
		open = $bindable(false),
		passkeySupported = true,
		hasLocalPassword,
		onResolved,
		onCancel
	}: {
		open: boolean;
		passkeySupported?: boolean;
		hasLocalPassword: boolean;
		onResolved: (grant: StepUpGrant) => Promise<void> | void;
		onCancel?: () => void;
	} = $props();

	let password = $state('');
	let busy = $state(false);
	let error = $state<string | null>(null);

	function reset() {
		password = '';
		busy = false;
		error = null;
	}

	function handleOpenChange(nextOpen: boolean) {
		open = nextOpen;
		if (!nextOpen) {
			reset();
			onCancel?.();
		}
	}

	async function stepUpWithPasskey() {
		const challenge = await passkeyService.beginStepUp();
		if (!challenge.transactionId) throw new Error(m.account_passkey_step_up_failed());
		const credential = await startAuthentication({ optionsJSON: challenge.options });
		const grant = await passkeyService.finishStepUp(challenge.transactionId, credential);
		await onResolved(grant);
		open = false;
	}

	async function resolveWithPasskey() {
		busy = true;
		error = null;
		try {
			const steppedUp = await tryCatch(stepUpWithPasskey());
			if (steppedUp.error !== null && !(steppedUp.error instanceof Error && steppedUp.error.name === 'NotAllowedError')) {
				error = steppedUp.error instanceof Error ? steppedUp.error.message : m.account_passkey_step_up_failed();
			}
		} finally {
			busy = false;
		}
	}

	async function resolveWithPassword(event: SubmitEvent) {
		event.preventDefault();
		if (!password.trim()) return;

		busy = true;
		error = null;
		try {
			const grant = await tryCatch(passkeyService.passwordStepUp(password));
			if (grant.error !== null) {
				error = grant.error instanceof Error ? grant.error.message : m.account_passkey_step_up_failed();
				return;
			}
			const resolved = await tryCatch(Promise.resolve(onResolved(grant.data)));
			if (resolved.error !== null) {
				error = resolved.error instanceof Error ? resolved.error.message : m.account_passkey_step_up_failed();
				return;
			}
			open = false;
		} finally {
			busy = false;
		}
	}
</script>

<Dialog.Root {open} onOpenChange={handleOpenChange}>
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header>
			<Dialog.Title>
				<span class="flex items-center gap-2">
					<ApiKeyIcon class="size-5 text-primary" />
					{m.account_passkey_step_up_title()}
				</span>
			</Dialog.Title>
			<Dialog.Description>{m.account_passkey_step_up_description()}</Dialog.Description>
		</Dialog.Header>

		{#if error}
			<Alert.Root variant="destructive" icon={AlertIcon} heading={m.auth_failed_title()} description={error} />
		{/if}

		<div class="space-y-4">
			{#if !passkeySupported && !hasLocalPassword}
				<Alert.Root variant="destructive">
					<AlertIcon class="size-4" />
					<Alert.Description>{m.account_passkey_step_up_unavailable()}</Alert.Description>
				</Alert.Root>
			{/if}

			{#if passkeySupported}
				<Button type="button" class="w-full" disabled={busy} onclick={() => void resolveWithPasskey()}>
					<ApiKeyIcon class="size-4" />
					{m.account_passkey_step_up_passkey()}
				</Button>
			{/if}

			{#if hasLocalPassword}
				<div class="relative flex items-center">
					<div class="w-full border-t border-border/60"></div>
					<span class="absolute left-1/2 -translate-x-1/2 rounded-full border bg-card px-3 py-1 text-xs text-muted-foreground">
						{m.auth_or_continue()}
					</span>
				</div>

				<form class="space-y-3" onsubmit={resolveWithPassword}>
					<Label for="step-up-password">{m.common_password()}</Label>
					<InputGroup.Root role={undefined}>
						<InputGroup.Input
							id="step-up-password"
							name="password"
							type="password"
							autocomplete="current-password"
							placeholder={m.account_passkey_step_up_password_placeholder()}
							bind:value={password}
							disabled={busy}
						/>
					</InputGroup.Root>
					<Button type="submit" variant="outline" class="w-full" disabled={busy || !password.trim()}>
						{m.account_passkey_step_up_password()}
					</Button>
				</form>
			{/if}
		</div>
	</Dialog.Content>
</Dialog.Root>
