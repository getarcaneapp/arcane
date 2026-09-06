<script lang="ts">
	import {
		browserSupportsWebAuthn,
		startRegistration,
		type PublicKeyCredentialCreationOptionsJSON
	} from '@simplewebauthn/browser';
	import { Temporal } from 'temporal-polyfill';
	import { onMount } from 'svelte';
	import { toast } from 'svelte-sonner';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { formatDate, parseInstant } from '#lib/utils/formatting.js';
	import { passkeyService } from '#lib/services/passkey-service.js';
	import type { MFAStatus, Passkey, PasskeyCapabilities, StepUpGrant } from '#lib/types/auth.js';
	import StepUpDialog from './step-up-dialog.svelte';
	import MFADialog from './mfa-dialog.svelte';
	import { AddIcon, AlertIcon, ApiKeyIcon, CopyIcon, EditIcon, ShieldCheckIcon, TrashIcon } from '#lib/icons/index.js';

	let passkeys = $state<Passkey[]>([]);
	let capabilities = $state<PasskeyCapabilities | null>(null);
	let mfaStatus = $state<MFAStatus | null>(null);
	let loading = $state(true);
	let loadError = $state('');
	let passkeySupported = $state(false);
	let passkeySupportChecked = $state(false);
	let adding = $state(false);
	let editingId = $state<string | null>(null);
	let editingName = $state('');
	let recoveryCodes = $state<string[]>([]);
	let stepUpOpen = $state(false);
	let mfaDialogOpen = $state(false);
	let pendingAction = $state<((token: string) => Promise<void>) | null>(null);
	// One step-up covers every management action until the grant expires, so a
	// run of changes (add passkey, enable MFA, regenerate codes) prompts once.
	let grantToken = $state('');
	let grantExpiresAt = $state<Temporal.Instant | null>(null);

	function passkeyErrorDescription(value: unknown, fallback: string): string {
		if (!(value instanceof Error) || !value.message.trim()) return fallback;
		const message = value.message.trim();
		if (
			[
				'passkey authentication failed',
				'authentication failed',
				'internal server error',
				'invalid or expired passkey authentication attempt'
			].includes(message.toLowerCase())
		) {
			return fallback;
		}
		return message;
	}

	function showPasskeyError(title: string, value: unknown, fallback: string) {
		// The grant may have been the reason the call failed; drop it so the next
		// attempt re-authenticates instead of replaying a dead token.
		clearStepUpGrant();
		toast.error(title, { description: passkeyErrorDescription(value, fallback) });
	}

	function clearStepUpGrant() {
		grantToken = '';
		grantExpiresAt = null;
	}

	function activeStepUpToken(): string {
		// Small margin so a grant does not expire between check and request.
		return grantToken &&
			grantExpiresAt &&
			Temporal.Instant.compare(grantExpiresAt.subtract({ seconds: 5 }), Temporal.Now.instant()) > 0
			? grantToken
			: '';
	}

	async function loadAccountData(): Promise<PasskeyCapabilities | null> {
		loading = true;
		loadError = '';
		try {
			const [passkeysResult, capabilitiesResult, mfaStatusResult] = await Promise.allSettled([
				passkeyService.listMine(),
				passkeyService.getCapabilities(),
				passkeyService.getMFAStatus()
			]);

			if (passkeysResult.status === 'fulfilled') passkeys = passkeysResult.value;
			if (capabilitiesResult.status === 'fulfilled') capabilities = capabilitiesResult.value;
			if (mfaStatusResult.status === 'fulfilled') mfaStatus = mfaStatusResult.value;

			const failure = [passkeysResult, capabilitiesResult, mfaStatusResult].find((result) => result.status === 'rejected');
			if (failure?.status === 'rejected') {
				loadError = passkeyErrorDescription(failure.reason, m.account_passkey_load_failed_description());
			}

			return capabilitiesResult.status === 'fulfilled' ? capabilitiesResult.value : capabilities;
		} finally {
			loading = false;
		}
	}

	async function refreshCapabilities(): Promise<PasskeyCapabilities | null> {
		try {
			const loadedCapabilities = await passkeyService.getCapabilities();
			capabilities = loadedCapabilities;
			return loadedCapabilities;
		} catch {
			return null;
		}
	}

	async function runWithStepUp(action: (token: string) => Promise<void>) {
		const currentCapabilities = capabilities ?? (await refreshCapabilities());
		if (!currentCapabilities) {
			if (passkeys.length === 0) {
				await action('');
				return;
			}
			pendingAction = action;
			stepUpOpen = true;
			return;
		}

		if (!currentCapabilities.requiresStepUp) {
			await action('');
			return;
		}

		const token = activeStepUpToken();
		if (token) {
			await action(token);
			return;
		}

		pendingAction = action;
		stepUpOpen = true;
	}

	async function handleStepUpResolved(grant: StepUpGrant) {
		grantToken = grant.token;
		grantExpiresAt = parseInstant(grant.expiresAt);
		const action = pendingAction;
		pendingAction = null;
		if (!action) return;
		try {
			await action(grant.token);
		} catch (value) {
			showPasskeyError(m.account_passkey_step_up_failed(), value, m.account_passkey_request_failed_description());
		}
	}

	function handleStepUpCancel() {
		pendingAction = null;
	}

	async function addPasskey() {
		await runWithStepUp(async (stepUpToken) => {
			adding = true;
			try {
				const challenge = await passkeyService.beginRegistration(stepUpToken || undefined);
				const credential = await startRegistration({
					optionsJSON: challenge.options as unknown as PublicKeyCredentialCreationOptionsJSON
				});
				const created = await passkeyService.finishRegistration(challenge.ceremonyId, credential);
				passkeys = [...passkeys, created];
				toast.success(m.account_passkey_added());
				await loadAccountData();
			} catch (value) {
				if (!(value instanceof Error && value.name === 'NotAllowedError')) {
					showPasskeyError(m.account_passkey_add_failed(), value, m.account_passkey_add_failed_description());
				}
			} finally {
				adding = false;
			}
		});
	}

	function startRename(passkey: Passkey) {
		editingId = passkey.id;
		editingName = passkey.name;
	}

	function cancelRename() {
		editingId = null;
		editingName = '';
	}

	async function renamePasskey(passkey: Passkey) {
		if (!editingName.trim()) {
			toast.error(m.account_passkey_name_required());
			return;
		}
		await runWithStepUp(async (stepUpToken) => {
			try {
				const updated = await passkeyService.rename(passkey.id, editingName, stepUpToken);
				passkeys = passkeys.map((item) => (item.id === updated.id ? updated : item));
				cancelRename();
				toast.success(m.account_passkey_renamed());
			} catch (value) {
				showPasskeyError(m.account_passkey_rename_failed(), value, m.account_passkey_request_failed_description());
			}
		});
	}

	function confirmDelete(passkey: Passkey) {
		openConfirmDialog({
			title: m.account_passkey_delete_title(),
			message: m.account_passkey_delete_description({ name: passkey.name }),
			confirm: {
				label: m.account_passkey_delete(),
				destructive: true,
				action: () => {
					void runWithStepUp(async (stepUpToken) => {
						try {
							await passkeyService.deleteMine(passkey.id, stepUpToken);
							passkeys = passkeys.filter((item) => item.id !== passkey.id);
							toast.success(m.account_passkey_deleted());
							await loadAccountData();
						} catch (value) {
							showPasskeyError(m.account_passkey_delete_failed(), value, m.account_passkey_request_failed_description());
						}
					});
				}
			}
		});
	}

	async function enableMFA() {
		await runWithStepUp(async (stepUpToken) => {
			try {
				const generated = await passkeyService.enableMFA(stepUpToken);
				recoveryCodes = generated.codes;
				toast.success(m.account_passkey_mfa_enabled());
				await loadAccountData();
			} catch (value) {
				showPasskeyError(m.account_passkey_mfa_enable_failed(), value, m.account_passkey_request_failed_description());
			}
		});
	}

	async function disableMFA() {
		await runWithStepUp(async (stepUpToken) => {
			try {
				await passkeyService.disableMFA(stepUpToken);
				recoveryCodes = [];
				toast.success(m.account_passkey_mfa_disabled());
				await loadAccountData();
			} catch (value) {
				showPasskeyError(m.account_passkey_mfa_disable_failed(), value, m.account_passkey_request_failed_description());
			}
		});
	}

	async function regenerateRecoveryCodes() {
		await runWithStepUp(async (stepUpToken) => {
			try {
				const generated = await passkeyService.regenerateRecoveryCodes(stepUpToken);
				recoveryCodes = generated.codes;
				await loadAccountData();
			} catch (value) {
				showPasskeyError(m.account_passkey_mfa_regenerate_failed(), value, m.account_passkey_request_failed_description());
			}
		});
	}

	async function copyRecoveryCodes() {
		try {
			await navigator.clipboard.writeText(recoveryCodes.join('\n'));
			toast.success(m.account_passkey_mfa_codes_copied());
		} catch {
			toast.error(m.common_copy_failed());
		}
	}

	function dismissRecoveryCodes() {
		recoveryCodes = [];
	}

	onMount(() => {
		passkeySupported = browserSupportsWebAuthn();
		passkeySupportChecked = true;
		void loadAccountData();
	});
</script>

<section class="space-y-5">
	<div class="flex flex-wrap items-start justify-between gap-3">
		<div class="min-w-0">
			<h2 class="text-base font-semibold tracking-tight sm:text-lg">{m.account_passkeys_title()}</h2>
			<p class="mt-1 text-xs text-muted-foreground sm:text-sm">{m.account_passkeys_description()}</p>
		</div>
		<div class="flex shrink-0 flex-wrap items-center justify-end gap-2">
			{#if mfaStatus}
				<ArcaneButton
					action={mfaStatus.enabled ? 'remove' : 'confirm'}
					tone={mfaStatus.enabled ? 'outline-destructive' : 'outline'}
					size="sm"
					icon={ShieldCheckIcon}
					customLabel={mfaStatus.enabled ? m.account_passkey_mfa_disable() : m.account_passkey_mfa_enable()}
					onclick={() => (mfaDialogOpen = true)}
				/>
			{/if}
			<ArcaneButton
				action="create"
				tone="outline"
				size="sm"
				customLabel={m.account_passkey_add()}
				icon={AddIcon}
				loading={adding}
				disabled={adding || !passkeySupported}
				onclick={() => void addPasskey()}
			/>
		</div>
	</div>

	<div class="space-y-5">
		{#if recoveryCodes.length > 0}
			<Alert.Root class="border-primary/30 bg-primary/5">
				<AlertIcon class="size-4 text-primary" />
				<Alert.Title>{m.account_passkey_mfa_recovery_codes()}</Alert.Title>
				<Alert.Description>
					<p>{m.account_passkey_mfa_recovery_codes_description()}</p>
					<p class="mt-2 font-medium">{m.account_passkey_mfa_codes_warning()}</p>
					<ul class="mt-3 grid gap-1 rounded border bg-background p-3 font-mono text-xs leading-6 sm:grid-cols-2">
						{#each recoveryCodes as code (code)}
							<li>{code}</li>
						{/each}
					</ul>
					<div class="mt-3 flex flex-wrap justify-end gap-2">
						<ArcaneButton
							action="base"
							tone="outline"
							size="sm"
							customLabel={m.account_passkey_mfa_copy_codes()}
							icon={CopyIcon}
							onclick={() => void copyRecoveryCodes()}
						/>
						<ArcaneButton
							action="cancel"
							tone="ghost"
							size="sm"
							customLabel={m.common_ive_saved_it()}
							onclick={dismissRecoveryCodes}
						/>
					</div>
				</Alert.Description>
			</Alert.Root>
		{/if}

		{#if loadError}
			<Alert.Root variant="destructive">
				<AlertIcon class="size-4" />
				<Alert.Title>{m.account_passkey_load_failed()}</Alert.Title>
				<Alert.Description class="space-y-3">
					<p>{loadError}</p>
					<ArcaneButton
						action="refresh"
						tone="outline"
						size="sm"
						customLabel={m.common_retry()}
						onclick={() => void loadAccountData()}
					/>
				</Alert.Description>
			</Alert.Root>
		{/if}

		<div class="overflow-hidden rounded-xl border border-border/60 bg-card/30">
			{#if loading}
				<div class="p-6 text-center text-sm text-muted-foreground">{m.common_loading()}</div>
			{:else if passkeys.length === 0}
				<div class="p-6 text-center text-sm text-muted-foreground">
					<ApiKeyIcon class="mx-auto mb-2 size-8 opacity-40" />
					{m.account_passkeys_empty()}
					{#if passkeySupportChecked && !passkeySupported}
						<p class="mx-auto mt-2 max-w-sm text-xs">{m.account_passkey_unsupported()}</p>
					{/if}
				</div>
			{:else}
				<ul class="divide-y divide-border/60">
					{#each passkeys as passkey (passkey.id)}
						<li class="px-4 py-3">
							{#if editingId === passkey.id}
								<form
									class="flex flex-wrap items-center gap-2"
									onsubmit={(event) => {
										event.preventDefault();
										void renamePasskey(passkey);
									}}
								>
									<Input class="min-w-0 flex-1" bind:value={editingName} aria-label={m.common_name()} />
									<Button type="submit" size="sm" disabled={!editingName.trim()}>{m.common_save()}</Button>
									<Button type="button" variant="ghost" size="sm" onclick={cancelRename}>{m.common_cancel()}</Button>
								</form>
							{:else}
								<div class="flex items-start justify-between gap-2">
									<div class="min-w-0 flex-1">
										<div class="truncate text-sm font-medium">{passkey.name}</div>
										<div class="mt-1 text-xs text-muted-foreground">
											{#if passkey.lastUsedAt}
												{m.account_passkey_last_used({ date: formatDate(passkey.lastUsedAt) || passkey.lastUsedAt })}
											{:else}
												{m.account_passkey_never_used()}
											{/if}
										</div>
									</div>
									<div class="flex shrink-0 items-center gap-1">
										<ArcaneButton
											action="edit"
											tone="ghost"
											size="sm"
											icon={EditIcon}
											customLabel={m.account_passkey_rename()}
											showLabel={false}
											onclick={() => startRename(passkey)}
										/>
										<ArcaneButton
											action="remove"
											tone="ghost"
											size="sm"
											icon={TrashIcon}
											customLabel={m.account_passkey_delete()}
											showLabel={false}
											class="text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
											onclick={() => confirmDelete(passkey)}
										/>
									</div>
								</div>
							{/if}
						</li>
					{/each}
				</ul>
			{/if}
		</div>
	</div>
</section>

{#if mfaStatus}
	<MFADialog
		bind:open={mfaDialogOpen}
		enabled={mfaStatus.enabled}
		passkeyCount={passkeys.length}
		recoveryCodesRemaining={mfaStatus.recoveryCodesRemaining}
		onEnable={() => void enableMFA()}
		onDisable={() => void disableMFA()}
		onRegenerate={() => void regenerateRecoveryCodes()}
	/>
{/if}

<StepUpDialog
	bind:open={stepUpOpen}
	passkeySupported={passkeySupported && passkeys.length > 0}
	hasLocalPassword={capabilities?.hasLocalPassword ?? false}
	onResolved={handleStepUpResolved}
	onCancel={handleStepUpCancel}
/>
