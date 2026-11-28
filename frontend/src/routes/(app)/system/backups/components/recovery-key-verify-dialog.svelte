<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import TextInputWithLabel from '#lib/components/form/text-input-with-label.svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Badge, type BadgeVariant } from '#lib/components/ui/badge/index.js';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog/index.js';
	import { AlertIcon, CloudStorageIcon, InfoIcon, ShieldCheckIcon, ShieldXIcon, VolumesIcon } from '#lib/icons/index.js';
	import * as m from '#lib/paraglide/messages.js';
	import { systemBackupService } from '#lib/services/system-backup-service.js';
	import type { RecoveryKeyCheckStatus, SystemBackupRecoveryKeyCheck } from '#lib/types/backup.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { backupDestinationLabel, backupDestinationName } from '#lib/utils/backups.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let { open = $bindable(false), recoveryKeyStored }: { open: boolean; recoveryKeyStored: boolean } = $props();

	const statusBadges: Record<RecoveryKeyCheckStatus, { label: () => string; variant: BadgeVariant }> = {
		valid: { label: m.key_opens, variant: 'green' },
		invalid: { label: m.wrong_key, variant: 'red' },
		missing: { label: m.system_backups_empty_title, variant: 'gray' },
		error: { label: m.unreachable, variant: 'amber' }
	};
	const textSecurity = typeof CSS !== 'undefined' && CSS.supports('-webkit-text-security', 'disc');

	let recoveryKey = $state('');
	let verifying = $state(false);
	let result = $state<SystemBackupRecoveryKeyCheck | null>(null);
	let session = 0;
	const keyMissing = $derived(!recoveryKeyStored && !recoveryKey.trim());
	const rejected = $derived(result?.repositories.filter((item) => item.status === 'invalid').length ?? 0);
	const unreachable = $derived(result?.repositories.filter((item) => item.status === 'error').length ?? 0);
	const opened = $derived(result?.repositories.filter((item) => item.status === 'valid').length ?? 0);

	function reset(nextOpen: boolean) {
		open = nextOpen;
		if (nextOpen) return;
		session += 1;
		recoveryKey = '';
		result = null;
	}

	async function verify(event?: SubmitEvent) {
		event?.preventDefault();
		if (verifying || keyMissing) return;
		result = null;
		verifying = true;
		const requestSession = session;
		await handleApiResultWithCallbacks({
			result: await tryCatch(systemBackupService.verifyRecoveryKey(recoveryKey.trim())),
			message: m.verify_recovery_key_failed(),
			setLoadingState: (value) => {
				verifying = value;
			},
			onSuccess: (data) => {
				if (requestSession === session) result = data;
			}
		});
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
		<TextInputWithLabel
			id="verify-recovery-key"
			name="verify-recovery-key"
			label={m.system_backups_recovery_key()}
			description={recoveryKeyStored
				? m.system_backups_recovery_key_saved_description()
				: m.system_backups_recovery_key_required()}
			type={textSecurity ? 'text' : 'password'}
			bind:value={recoveryKey}
			onChange={() => (result = null)}
			disabled={verifying}
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
				{#each result.repositories as item (`${item.type}:${item.destination}:${item.s3DestinationId ?? ''}:${item.instanceId ?? ''}`)}
					{@const badge = statusBadges[item.status]}
					{@const LocationIcon = item.destination === 'local' ? VolumesIcon : CloudStorageIcon}
					<li class="flex items-center justify-between gap-3 px-3 py-2.5">
						<div class="flex min-w-0 items-center gap-2.5">
							<LocationIcon class="size-4 shrink-0 text-muted-foreground" />
							<div class="min-w-0">
								<p class="truncate text-sm font-medium">
									{item.type === 'system' ? m.jobs_system_backup_group() : m.system_volume_backups_title()}
								</p>
								<p class="truncate text-xs text-muted-foreground" title={item.error}>
									{backupDestinationName(item) || backupDestinationLabel(item.destination)}
									{#if item.instanceId}· {m.instance_id({ id: item.instanceId })}{/if}
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
		<ArcaneButton action="cancel" customLabel={m.common_close()} onclick={() => reset(false)} disabled={verifying} />
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
