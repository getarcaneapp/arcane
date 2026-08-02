<script lang="ts">
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog';
	import { ArcaneButton } from '#lib/components/arcane-button';
	import BackupPolicyFields from '#lib/components/backup-policy-fields.svelte';
	import type { BackupPolicyForm } from '#lib/types/backup';
	import type { UpdateVolumeBackupPolicy, VolumeBackupPolicy } from '#lib/types/shared';
	import type { S3Destination } from '#lib/types/s3-destination';
	import { s3DestinationService } from '#lib/services/s3-destination-service';
	import { volumeBackupService } from '#lib/services/volume-backup-service';
	import { backupDestinationFromFlags, backupPolicyDestinationValues } from '#lib/utils/backups';
	import { toast } from 'svelte-sonner';
	import * as m from '#lib/paraglide/messages.js';

	type PolicyForm = UpdateVolumeBackupPolicy & BackupPolicyForm & { serverError?: string };

	let {
		open = $bindable(),
		volumeName,
		policies,
		policyId,
		onSaved
	}: {
		open: boolean;
		volumeName: string;
		policies: VolumeBackupPolicy[];
		policyId?: string;
		onSaved: (policies: VolumeBackupPolicy[]) => void;
	} = $props();

	let saving = $state(false);
	let deleting = $state(false);
	let s3Destinations = $state<S3Destination[]>([]);
	let s3DestinationsLoading = $state(false);
	let form = $state<PolicyForm>(newPolicy());
	const scheduleError = $derived(
		!form.schedule.trim()
			? m.jobs_cron_required()
			: form.schedule.trim().split(/\s+/).length !== 6
				? m.jobs_cron_invalid()
				: (form.serverError ?? null)
	);
	const retentionError = $derived(
		Number.isInteger(Number(form.retentionCount)) && form.retentionCount >= 0 && form.retentionCount <= 3650
			? null
			: m.volume_backup_retention_invalid()
	);
	const destinationError = $derived(
		form.destination === 'local' || s3DestinationsLoading
			? null
			: !form.s3DestinationId
				? m.volume_backup_s3_destination_required()
				: s3Destinations.some((item) => item.id === form.s3DestinationId)
					? null
					: m.volume_backup_destination_unavailable()
	);
	const formInvalid = $derived(Boolean(scheduleError || retentionError || destinationError));

	function newPolicy(): PolicyForm {
		return {
			id: '',
			enabled: true,
			schedule: '0 0 2 * * *',
			retentionCount: 7,
			stopContainers: false,
			localEnabled: true,
			s3Enabled: false,
			s3DestinationId: '',
			destination: 'local'
		};
	}

	async function loadS3Destinations() {
		s3DestinationsLoading = true;
		try {
			s3Destinations = await s3DestinationService.listAll();
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.s3_destinations_load_failed());
		} finally {
			s3DestinationsLoading = false;
		}
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
					stopContainers: policy.stopContainers,
					localEnabled: policy.localEnabled,
					s3Enabled: policy.s3Enabled,
					s3DestinationId: policy.s3DestinationId || '',
					destination: backupDestinationFromFlags(policy.localEnabled, policy.s3Enabled)
				}
			: newPolicy();
		void loadS3Destinations();
	});

	function updateForm(values: Partial<BackupPolicyForm>) {
		form = { ...form, ...values, serverError: undefined };
	}

	function policyUpdatePayload(policy: VolumeBackupPolicy): UpdateVolumeBackupPolicy {
		return {
			id: policy.id,
			enabled: policy.enabled,
			schedule: policy.schedule,
			retentionCount: policy.retentionCount,
			stopContainers: policy.stopContainers,
			localEnabled: policy.localEnabled,
			s3Enabled: policy.s3Enabled,
			s3DestinationId: policy.s3DestinationId
		};
	}

	async function savePolicies() {
		if (formInvalid) return;
		saving = true;
		try {
			const { destination, serverError: _, ...values } = form;
			const currentPolicy: UpdateVolumeBackupPolicy = {
				...values,
				retentionCount: Number(values.retentionCount),
				...backupPolicyDestinationValues(destination, values.s3DestinationId)
			};
			const existingPolicies = policies.map(policyUpdatePayload);
			const nextPolicies = policyId
				? existingPolicies.map((policy) => (policy.id === policyId ? currentPolicy : policy))
				: [...existingPolicies, currentPolicy];
			const updated = await volumeBackupService.updatePolicies(volumeName, nextPolicies);
			onSaved(updated.policies);
			open = false;
			toast.success(m.volume_backup_policy_saved());
		} catch (error) {
			const message = error instanceof Error ? error.message : m.volume_backup_policy_save_failed();
			if (/cron|schedule/i.test(message)) form = { ...form, serverError: message };
			else toast.error(message);
		} finally {
			saving = false;
		}
	}

	async function deletePolicy() {
		if (!policyId) return;
		deleting = true;
		try {
			const remainingPolicies = policies.filter((policy) => policy.id !== policyId).map(policyUpdatePayload);
			const updated = await volumeBackupService.updatePolicies(volumeName, remainingPolicies);
			onSaved(updated.policies);
			open = false;
			toast.success(m.volume_backup_schedule_removed());
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.volume_backup_policy_save_failed());
		} finally {
			deleting = false;
		}
	}
</script>

<ResponsiveDialog
	bind:open
	title={policyId ? m.jobs_edit_schedule() : m.volume_backup_add_schedule()}
	description={m.volume_backup_policy_description()}
	contentClass="sm:max-w-[760px]"
>
	{#snippet children()}
		<div class="space-y-4 py-2">
			<BackupPolicyFields
				idPrefix="volume-backup-policy"
				{form}
				destinations={s3Destinations}
				{scheduleError}
				{retentionError}
				{destinationError}
				enabledDescription={m.volume_backup_policy_enabled_description()}
				schedulePlaceholder="0 0 2 * * *"
				showStopContainers
				destinationsLoading={s3DestinationsLoading}
				onChange={updateForm}
			/>
		</div>
	{/snippet}
	{#snippet footer()}
		{#if policyId}
			<ArcaneButton
				action="remove"
				customLabel={m.backups_remove_schedule()}
				onclick={deletePolicy}
				loading={deleting}
				disabled={saving || deleting}
			/>
		{/if}
		<ArcaneButton action="cancel" onclick={() => (open = false)} />
		<ArcaneButton action="save" onclick={savePolicies} loading={saving} disabled={saving || deleting || formInvalid} />
	{/snippet}
</ResponsiveDialog>
