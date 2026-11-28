<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Badge, type BadgeVariant } from '#lib/components/ui/badge/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog/index.js';
	import { AlertIcon, CloudStorageIcon, InfoIcon, ShieldCheckIcon, ShieldXIcon, VolumesIcon } from '#lib/icons/index.js';
	import * as m from '#lib/paraglide/messages.js';
	import { systemBackupService } from '#lib/services/system-backup-service.js';
	import type { BackupRepositoryKeyCheck, RecoveryKeyCheckStatus, SystemBackupRecoveryKeyCheck } from '#lib/types/backup.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let { open = $bindable(false), recoveryKeyStored }: { open: boolean; recoveryKeyStored: boolean } = $props();

	const statusBadges: Record<RecoveryKeyCheckStatus, { label: () => string; variant: BadgeVariant }> = {
		valid: { label: m.key_opens, variant: 'green' },
		invalid: { label: m.wrong_key, variant: 'red' },
		missing: { label: m.no_backups_yet, variant: 'gray' },
		error: { label: m.unreachable, variant: 'amber' }
	};
	const textSecurity = typeof CSS !== 'undefined' && CSS.supports('-webkit-text-security', 'disc');

	let recoveryKey = $state('');
	let verifying = $state(false);
	let result = $state<SystemBackupRecoveryKeyCheck | null>(null);
	const keyMissing = $derived(!recoveryKeyStored && !recoveryKey.trim());
	const rejected = $derived(result?.repositories.filter((item) => item.status === 'invalid').length ?? 0);
	const unreachable = $derived(result?.repositories.filter((item) => item.status === 'error').length ?? 0);
	const opened = $derived(result?.repositories.filter((item) => item.status === 'valid').length ?? 0);

	function reset(nextOpen: boolean) {
		if (nextOpen) return;
		recoveryKey = '';
		result = null;
	}

	function changeKey(event: Event) {
		recoveryKey = (event.target as HTMLInputElement).value;
		result = null;
	}

	async function verify(event?: SubmitEvent) {
		event?.preventDefault();
		if (verifying || keyMissing) return;
		result = null;
		await handleApiResultWithCallbacks({
			result: await tryCatch(systemBackupService.verifyRecoveryKey(recoveryKey.trim())),
			message: m.verify_recovery_key_failed(),
			setLoadingState: (value) => {
				verifying = value;
			},
			onSuccess: (data) => {
				result = data;
			}
		});
	}

	function repositoryLocation(item: BackupRepositoryKeyCheck) {
		if (item.destination === 'local') return m.local();
		return item.s3DestinationName || item.s3DestinationId || 'S3';
	}
</script>

<ResponsiveDialog
	bind:open
	onOpenChange={reset}
	title={m.verify_recovery_key()}
	description={m.verify_recovery_key_description()}
	contentClass="sm:max-w-160"
>
	<form id="verify-recovery-key-form" class="space-y-4 py-2" onsubmit={verify}>
		<div class="space-y-2.5">
			<div>
				<Label for="verify-recovery-key">{m.system_backups_recovery_key()}</Label>
				<p class="mt-0.5 text-xs text-muted-foreground">
					{recoveryKeyStored ? m.system_backups_recovery_key_saved_description() : m.system_backups_recovery_key_required()}
				</p>
			</div>
			<Input
				id="verify-recovery-key"
				name="verify-recovery-key"
				type={textSecurity ? 'text' : 'password'}
				value={recoveryKey}
				oninput={changeKey}
				mono
				autocomplete="off"
				autocapitalize="none"
				autocorrect="off"
				spellcheck={false}
				data-1p-ignore
				data-lpignore="true"
				data-bwignore
				data-form-type="other"
				data-protonpass-ignore
				class="text-security-disc"
			/>
		</div>

		{#if result}
			{#if opened === 0 && rejected === 0 && unreachable === 0}
				<Alert.Root size="sm">
					<InfoIcon class="size-4" />
					<Alert.Description>{m.recovery_key_no_repositories()}</Alert.Description>
				</Alert.Root>
			{:else if rejected > 0}
				<Alert.Root variant="destructive" size="sm">
					<ShieldXIcon class="size-4" />
					<Alert.Description>
						{rejected === 1
							? m.recovery_key_rejected_by_repositories_one()
							: m.recovery_key_rejected_by_repositories({ count: rejected })}
					</Alert.Description>
				</Alert.Root>
			{:else if unreachable === 0}
				<Alert.Root variant="primary-subtle" size="sm">
					<ShieldCheckIcon class="size-4" />
					<Alert.Description>{m.recovery_key_opens_all_repositories()}</Alert.Description>
				</Alert.Root>
			{/if}
			{#if unreachable > 0}
				<Alert.Root variant="warning" size="sm">
					<AlertIcon class="size-4" />
					<Alert.Description>
						{unreachable === 1
							? m.recovery_key_unchecked_repositories_one()
							: m.recovery_key_unchecked_repositories({ count: unreachable })}
					</Alert.Description>
				</Alert.Root>
			{/if}

			<ul class="divide-y rounded-md border" data-testid="recovery-key-verify-results">
				{#each result.repositories as item (`${item.type}:${item.destination}:${item.s3DestinationId ?? ''}`)}
					{@const badge = statusBadges[item.status]}
					{@const LocationIcon = item.destination === 'local' ? VolumesIcon : CloudStorageIcon}
					<li class="flex items-center justify-between gap-3 px-3 py-2.5">
						<div class="flex min-w-0 items-center gap-2.5">
							<LocationIcon class="size-4 shrink-0 text-muted-foreground" />
							<div class="min-w-0">
								<p class="truncate text-sm font-medium">
									{item.type === 'system' ? m.system_backups() : m.volume_backups()}
								</p>
								<p class="truncate text-xs text-muted-foreground" title={item.error}>
									{repositoryLocation(item)}
									{#if item.status === 'valid'}· {item.snapshots === 1
											? m.snapshot_count_one()
											: m.snapshot_count({ count: item.snapshots })}{/if}
									{#if item.error}· {item.error}{/if}
								</p>
							</div>
						</div>
						<Badge variant={badge.variant} size="sm" class="shrink-0">{badge.label()}</Badge>
					</li>
				{/each}
			</ul>
		{/if}
	</form>

	{#snippet footer()}
		<ArcaneButton action="cancel" customLabel={m.common_close()} onclick={() => (open = false)} disabled={verifying} />
		<ArcaneButton
			action="confirm"
			icon={ShieldCheckIcon}
			customLabel={m.verify_recovery_key()}
			onclick={() => verify()}
			loading={verifying}
			disabled={verifying || keyMissing}
		/>
	{/snippet}
</ResponsiveDialog>
