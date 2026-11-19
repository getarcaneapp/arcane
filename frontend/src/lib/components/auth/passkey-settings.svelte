<script lang="ts">
	import { browserSupportsWebAuthn, startRegistration } from '@simplewebauthn/browser';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { onMount } from 'svelte';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { AddIcon, AlertIcon, ApiKeyIcon, CopyIcon, EditIcon, ShieldCheckIcon, TrashIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { mfaStatusQuery, passkeyCapabilitiesQuery, passkeysQuery } from '#lib/query/auth.js';
	import { passkeyService } from '#lib/services/passkey-service.js';
	import type { MFAStatus, Passkey, PasskeyCapabilities, StepUpGrant } from '#lib/types/auth.js';
	import { formatDate, parseInstant } from '#lib/utils/formatting.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import MFADialog from './mfa-dialog.svelte';
	import StepUpDialog from './step-up-dialog.svelte';

	const queryClient = useQueryClient();
	const accountQueryOptions = {
		staleTime: 0,
		retry: false,
		refetchOnWindowFocus: false,
		refetchOnReconnect: false
	} as const;
	const passkeysListQuery = createQuery(() => ({ ...passkeysQuery(), ...accountQueryOptions }));
	const capabilitiesQuery = createQuery(() => ({ ...passkeyCapabilitiesQuery(), ...accountQueryOptions }));
	const mfaQuery = createQuery(() => ({ ...mfaStatusQuery(), ...accountQueryOptions }));
	const passkeys = $derived<Passkey[]>(passkeysListQuery.data ?? []);
	const capabilities = $derived<PasskeyCapabilities | null>(capabilitiesQuery.data ?? null);
	const mfaStatus = $derived<MFAStatus | null>(mfaQuery.data ?? null);
	const loading = $derived(passkeysListQuery.isFetching || capabilitiesQuery.isFetching || mfaQuery.isFetching);
	const loadError = $derived.by(() => {
		const failed = [passkeysListQuery, capabilitiesQuery, mfaQuery].find((query) => query.isError);
		return failed ? passkeyErrorDescription(failed.error, m.account_passkey_load_failed_description()) : '';
	});
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
		if (!grantToken || !grantExpiresAt) return '';
		if (Temporal.Instant.compare(grantExpiresAt.subtract({ seconds: 5 }), Temporal.Now.instant()) <= 0) return '';
		return grantToken;
	}

	async function loadAccountData() {
		await Promise.all([passkeysListQuery.refetch(), capabilitiesQuery.refetch(), mfaQuery.refetch()]);
	}

	async function refreshCapabilities(): Promise<PasskeyCapabilities | null> {
		const result = await capabilitiesQuery.refetch();
		return result.data ?? null;
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

		const actionResult = await tryCatch(action(grant.token));
		if (actionResult.error !== null) {
			showPasskeyError(m.account_passkey_step_up_failed(), actionResult.error, m.account_passkey_request_failed_description());
		}
	}

	function handleStepUpCancel() {
		pendingAction = null;
	}

	async function registerPasskey(stepUpToken: string) {
		const challenge = await passkeyService.beginRegistration(stepUpToken || undefined);
		const credential = await startRegistration({ optionsJSON: challenge.options });
		const created = await passkeyService.finishRegistration(challenge.ceremonyId, credential);
		queryClient.setQueryData(passkeysQuery().queryKey, (current = []) => [...current, created]);
		toast.success(m.account_passkey_added());
		await loadAccountData();
	}

	async function addPasskey() {
		await runWithStepUp(async (stepUpToken) => {
			adding = true;
			try {
				const registered = await tryCatch(registerPasskey(stepUpToken));
				if (registered.error !== null && !(registered.error instanceof Error && registered.error.name === 'NotAllowedError')) {
					showPasskeyError(m.account_passkey_add_failed(), registered.error, m.account_passkey_add_failed_description());
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
			const renameResult = await tryCatch(passkeyService.rename(passkey.id, editingName, stepUpToken));
			if (renameResult.error !== null) {
				showPasskeyError(m.account_passkey_rename_failed(), renameResult.error, m.account_passkey_request_failed_description());
				return;
			}
			const updated = renameResult.data;
			queryClient.setQueryData(passkeysQuery().queryKey, (current = []) =>
				current.map((item) => (item.id === updated.id ? updated : item))
			);
			cancelRename();
			toast.success(m.account_passkey_renamed());
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
						const deleted = await tryCatch(passkeyService.deleteMine(passkey.id, stepUpToken));
						if (deleted.error !== null) {
							showPasskeyError(m.account_passkey_delete_failed(), deleted.error, m.account_passkey_request_failed_description());
							return;
						}
						queryClient.setQueryData(passkeysQuery().queryKey, (current = []) =>
							current.filter((item) => item.id !== passkey.id)
						);
						toast.success(m.account_passkey_deleted());
						const reloaded = await tryCatch(loadAccountData());
						if (reloaded.error !== null) {
							showPasskeyError(m.account_passkey_delete_failed(), reloaded.error, m.account_passkey_request_failed_description());
						}
					});
				}
			}
		});
	}

	async function enableMFA() {
		await runWithStepUp(async (stepUpToken) => {
			const generated = await tryCatch(passkeyService.enableMFA(stepUpToken));
			if (generated.error !== null) {
				showPasskeyError(m.account_passkey_mfa_enable_failed(), generated.error, m.account_passkey_request_failed_description());
				return;
			}
			recoveryCodes = generated.data.codes;
			toast.success(m.account_passkey_mfa_enabled());
			const reloaded = await tryCatch(loadAccountData());
			if (reloaded.error !== null) {
				showPasskeyError(m.account_passkey_mfa_enable_failed(), reloaded.error, m.account_passkey_request_failed_description());
			}
		});
	}

	async function disableMFA() {
		await runWithStepUp(async (stepUpToken) => {
			const disabled = await tryCatch(passkeyService.disableMFA(stepUpToken));
			if (disabled.error !== null) {
				showPasskeyError(m.account_passkey_mfa_disable_failed(), disabled.error, m.account_passkey_request_failed_description());
				return;
			}
			recoveryCodes = [];
			toast.success(m.account_passkey_mfa_disabled());
			const reloaded = await tryCatch(loadAccountData());
			if (reloaded.error !== null) {
				showPasskeyError(m.account_passkey_mfa_disable_failed(), reloaded.error, m.account_passkey_request_failed_description());
			}
		});
	}

	async function regenerateRecoveryCodes() {
		await runWithStepUp(async (stepUpToken) => {
			const generated = await tryCatch(passkeyService.regenerateRecoveryCodes(stepUpToken));
			if (generated.error !== null) {
				showPasskeyError(
					m.account_passkey_mfa_regenerate_failed(),
					generated.error,
					m.account_passkey_request_failed_description()
				);
				return;
			}
			recoveryCodes = generated.data.codes;
			const reloaded = await tryCatch(loadAccountData());
			if (reloaded.error !== null) {
				showPasskeyError(
					m.account_passkey_mfa_regenerate_failed(),
					reloaded.error,
					m.account_passkey_request_failed_description()
				);
			}
		});
	}

	async function copyRecoveryCodes() {
		const copyResult = await tryCatch(Promise.resolve().then(() => navigator.clipboard.writeText(recoveryCodes.join('\n'))));
		if (copyResult.error !== null) {
			toast.error(m.common_copy_failed());
			return;
		}
		toast.success(m.account_passkey_mfa_codes_copied());
	}

	function dismissRecoveryCodes() {
		recoveryCodes = [];
	}

	onMount(() => {
		passkeySupported = browserSupportsWebAuthn();
		passkeySupportChecked = true;
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
					icon={ShieldCheckIcon}
					customLabel={mfaStatus.enabled ? m.account_passkey_mfa_disable() : m.account_passkey_mfa_enable()}
					onclick={() => (mfaDialogOpen = true)}
				/>
			{/if}
			<ArcaneButton
				action="create"
				tone="outline"
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
			<Alert.Root variant="primary-subtle">
				<AlertIcon class="size-4 text-primary" />
				<Alert.Title>{m.account_passkey_mfa_recovery_codes()}</Alert.Title>
				<Alert.Description>
					<p>{m.account_passkey_mfa_recovery_codes_description()}</p>
					<p class="mt-2 font-medium">{m.account_passkey_mfa_codes_warning()}</p>
					<ul class="mt-3 grid gap-1 rounded-xl border bg-background p-3 font-mono text-xs leading-6 sm:grid-cols-2">
						{#each recoveryCodes as code (code)}
							<li>{code}</li>
						{/each}
					</ul>
					<div class="mt-3 flex flex-wrap justify-end gap-2">
						<ArcaneButton
							action="base"
							tone="outline"
							customLabel={m.account_passkey_mfa_copy_codes()}
							icon={CopyIcon}
							onclick={() => void copyRecoveryCodes()}
						/>
						<ArcaneButton action="cancel" tone="ghost" customLabel={m.common_ive_saved_it()} onclick={dismissRecoveryCodes} />
					</div>
				</Alert.Description>
			</Alert.Root>
		{/if}

		{#if loadError}
			<Alert.Root variant="destructive">
				<AlertIcon class="size-4" />
				<Alert.Title>{m.account_passkey_load_failed()}</Alert.Title>
				<Alert.Description>
					<div class="space-y-3">
						<p>{loadError}</p>
						<ArcaneButton action="refresh" tone="outline" customLabel={m.common_retry()} onclick={() => void loadAccountData()} />
					</div>
				</Alert.Description>
			</Alert.Root>
		{/if}

		<div class="overflow-hidden rounded-xl border border-border/70 bg-card/10">
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
											size="icon"
											icon={EditIcon}
											customLabel={m.account_passkey_rename()}
											showLabel={false}
											onclick={() => startRename(passkey)}
										/>
										<ArcaneButton
											action="remove"
											tone="ghost"
											size="icon"
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
