<script lang="ts">
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog';
	import { ArcaneButton } from '#lib/components/arcane-button';
	import BackupPolicyFields from '#lib/components/backup-policy-fields.svelte';
	import type { BackupPolicyForm } from '#lib/types/backup';
	import type { S3Destination } from '#lib/types/s3-destination';
	import type { SystemBackupPolicy, UpdateSystemBackupPolicy } from '#lib/types/system-backup';
	import { systemBackupService } from '#lib/services/system-backup-service';
	import { backupDestinationFromFlags, backupPolicyDestinationValues } from '#lib/utils/backups';
	import { toast } from 'svelte-sonner';
	import * as m from '#lib/paraglide/messages.js';

	type PolicyForm = UpdateSystemBackupPolicy & BackupPolicyForm & { serverError?: string };

	let {
		open = $bindable(),
		policies,
		policyId,
		recoveryKeyStored,
		destinations,
		onSaved
	}: {
		open: boolean;
		policies: SystemBackupPolicy[];
		policyId?: string;
		recoveryKeyStored: boolean;
		destinations: S3Destination[];
		onSaved: (policies: SystemBackupPolicy[]) => void;
	} = $props();

	let saving = $state(false);
	let deleting = $state(false);
	let form = $state<PolicyForm>(newPolicy());

	function newPolicy(): PolicyForm {
		return {
			id: '',
			enabled: recoveryKeyStored,
			schedule: '0 0 3 * * *',
			retentionCount: 7,
			localEnabled: true,
			s3Enabled: false,
			s3DestinationId: '',
			destination: 'local'
		};
	}

	$effect(() => {
		if (!open) return;
		const policy = policies.find((item) => item.id === policyId);
		form = policy
			? {
					id: policy.id,
					enabled: policy.enabled,
					schedule: policy.schedule,
					retentionCount: policy.retentionCount,
					localEnabled: policy.localEnabled,
					s3Enabled: policy.s3Enabled,
					s3DestinationId: policy.s3DestinationId || '',
					destination: backupDestinationFromFlags(policy.localEnabled, policy.s3Enabled)
				}
			: newPolicy();
	});

	const scheduleError = $derived(
		!form.schedule.trim() || form.schedule.trim().split(/\s+/).length !== 6 ? m.jobs_cron_invalid() : form.serverError || ''
	);
	const retentionError = $derived(
		Number.isInteger(Number(form.retentionCount)) && form.retentionCount >= 0 && form.retentionCount <= 3650
			? ''
			: m.volume_backup_retention_invalid()
	);
	const destinationError = $derived(
		form.destination !== 'local' && !form.s3DestinationId ? m.volume_backup_s3_destination_required() : ''
	);
	const keyError = $derived(form.enabled && !recoveryKeyStored ? m.system_backups_recovery_key_schedule_required() : '');
	const invalid = $derived(Boolean(scheduleError || retentionError || destinationError || keyError));

	function payload(policy: SystemBackupPolicy): UpdateSystemBackupPolicy {
		return {
			id: policy.id,
			enabled: policy.enabled,
			schedule: policy.schedule,
			retentionCount: policy.retentionCount,
			localEnabled: policy.localEnabled,
			s3Enabled: policy.s3Enabled,
			s3DestinationId: policy.s3DestinationId
		};
	}

	async function save() {
		if (invalid) return;
		saving = true;
		try {
			const { destination, serverError: _, ...values } = form;
			const current: UpdateSystemBackupPolicy = {
				...values,
				retentionCount: Number(values.retentionCount),
				...backupPolicyDestinationValues(destination, values.s3DestinationId)
			};
			const existing = policies.map(payload);
			const next = policyId ? existing.map((item) => (item.id === policyId ? current : item)) : [...existing, current];
			const updated = await systemBackupService.updatePolicies(next);
			onSaved(updated.policies);
			open = false;
			toast.success(m.system_backups_policy_saved());
		} catch (error) {
			const message = error instanceof Error ? error.message : m.system_backups_policy_failed();
			if (/cron|schedule/i.test(message)) form = { ...form, serverError: message };
			else toast.error(message);
		} finally {
			saving = false;
		}
	}

	async function remove() {
		if (!policyId) return;
		deleting = true;
		try {
			const updated = await systemBackupService.updatePolicies(policies.filter((item) => item.id !== policyId).map(payload));
			onSaved(updated.policies);
			open = false;
			toast.success(m.system_backups_schedule_removed());
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.system_backups_policy_failed());
		} finally {
			deleting = false;
		}
	}
</script>

<ResponsiveDialog
	bind:open
	title={policyId ? m.jobs_edit_schedule() : m.system_backups_add_schedule()}
	description={m.system_backups_schedule_description()}
	contentClass="sm:max-w-[680px]"
>
	{#snippet children()}
		<div class="space-y-5 py-2">
			<BackupPolicyFields
				idPrefix="system-backup-policy"
				{form}
				{destinations}
				{scheduleError}
				{retentionError}
				{destinationError}
				enabledError={keyError}
				enabledDescription={m.system_backups_enabled_description()}
				schedulePlaceholder="0 0 3 * * *"
				onChange={(values) => (form = { ...form, ...values, serverError: undefined })}
			/>
		</div>
	{/snippet}
	{#snippet footer()}
		{#if policyId}<ArcaneButton
				action="remove"
				customLabel={m.backups_remove_schedule()}
				onclick={remove}
				loading={deleting}
				disabled={saving || deleting}
			/>{/if}
		<ArcaneButton action="cancel" onclick={() => (open = false)} />
		<ArcaneButton action="save" onclick={save} loading={saving} disabled={saving || deleting || invalid} />
	{/snippet}
</ResponsiveDialog>
