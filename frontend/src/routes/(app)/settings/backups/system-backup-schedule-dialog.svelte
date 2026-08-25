<script lang="ts">
	import { toast } from 'svelte-sonner';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog';
	import { ArcaneButton } from '#lib/components/arcane-button';
	import BackupPolicyFields from '#lib/components/backup-policy-fields.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import { systemBackupService } from '#lib/services/system-backup-service';
	import type { BackupPolicyForm } from '#lib/types/backup';
	import type { S3Destination } from '#lib/types/s3-destination';
	import type {
		SystemBackupPolicy,
		SystemVolumeBackupOption,
		SystemVolumeBackupPolicy,
		SystemVolumeBackupSelectionMode,
		UpdateSystemBackupPolicy,
		UpdateSystemVolumeBackupPolicy
	} from '#lib/types/system-backup';
	import { backupDestinationFromFlags, backupPolicyDestinationValues } from '#lib/utils/backups';
	import SystemVolumeScopeFields from './system-volume-scope-fields.svelte';
	import * as m from '#lib/paraglide/messages.js';

	type BackupType = 'system' | 'volume';

	let {
		open = $bindable(),
		initialType,
		policyId,
		systemPolicies,
		volumePolicies,
		recoveryKeyStored,
		destinations,
		onSystemSaved,
		onVolumeSaved
	}: {
		open: boolean;
		initialType: BackupType;
		policyId?: string;
		systemPolicies: SystemBackupPolicy[];
		volumePolicies: SystemVolumeBackupPolicy[];
		recoveryKeyStored: boolean;
		destinations: S3Destination[];
		onSystemSaved: (policies: SystemBackupPolicy[]) => void;
		onVolumeSaved: (policies: SystemVolumeBackupPolicy[]) => void;
	} = $props();

	let backupType = $state<BackupType>('system');
	let form = $state<BackupPolicyForm>(newForm('system'));
	let selectionMode = $state<SystemVolumeBackupSelectionMode>('all');
	let volumeNames = $state<string[]>([]);
	let ignoreAnonymous = $state(true);
	let options = $state<SystemVolumeBackupOption[]>([]);
	let optionsLoading = $state(false);
	let saving = $state(false);
	let deleting = $state(false);
	let serverError = $state('');

	const editing = $derived(Boolean(policyId));
	const typeOptions = $derived([
		{ label: m.system(), value: 'system', description: m.system_backups_type_system_description() },
		{ label: m.resource_volume_cap(), value: 'volume', description: m.system_backups_type_volume_description() }
	]);
	const scheduleError = $derived(
		!form.schedule.trim()
			? m.jobs_cron_required()
			: form.schedule.trim().split(/\s+/).length !== 6
				? m.jobs_cron_invalid()
				: serverError || null
	);
	const retentionError = $derived(
		Number.isInteger(Number(form.retentionCount)) && form.retentionCount >= 0 && form.retentionCount <= 3650
			? null
			: m.volume_backup_retention_invalid()
	);
	const destinationError = $derived(
		form.destination !== 'local' && !form.s3DestinationId ? m.volume_backup_s3_destination_required() : null
	);
	const enabledError = $derived(
		backupType === 'system' && form.enabled && !recoveryKeyStored ? m.system_backups_recovery_key_schedule_required() : null
	);
	const invalid = $derived(Boolean(scheduleError || retentionError || destinationError || enabledError));

	function newForm(type: BackupType): BackupPolicyForm {
		return {
			enabled: type === 'system' ? recoveryKeyStored : true,
			schedule: type === 'system' ? '0 0 3 * * *' : '0 0 2 * * *',
			retentionCount: 7,
			stopContainers: false,
			destination: 'local',
			s3DestinationId: ''
		};
	}

	function policyForm(policy: SystemBackupPolicy | SystemVolumeBackupPolicy): BackupPolicyForm {
		return {
			enabled: policy.enabled,
			schedule: policy.schedule,
			retentionCount: policy.retentionCount,
			stopContainers: 'stopContainers' in policy ? (policy.stopContainers ?? false) : false,
			destination: backupDestinationFromFlags(policy.localEnabled, policy.s3Enabled),
			s3DestinationId: policy.s3DestinationId ?? ''
		};
	}

	async function loadVolumeOptions() {
		if (optionsLoading) return;
		optionsLoading = true;
		try {
			options = await systemBackupService.listSystemVolumeOptions();
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.system_volume_backups_options_failed());
		} finally {
			optionsLoading = false;
		}
	}

	$effect(() => {
		if (!open) return;
		backupType = initialType;
		const systemPolicy = initialType === 'system' ? systemPolicies.find((item) => item.id === policyId) : undefined;
		const volumePolicy = initialType === 'volume' ? volumePolicies.find((item) => item.id === policyId) : undefined;
		const policy = systemPolicy ?? volumePolicy;
		form = policy ? policyForm(policy) : newForm(initialType);
		selectionMode = volumePolicy?.selectionMode ?? 'all';
		volumeNames = volumePolicy ? [...volumePolicy.volumeNames] : [];
		ignoreAnonymous = volumePolicy?.ignoreAnonymous ?? true;
		serverError = '';
		if (initialType === 'volume') void loadVolumeOptions();
	});

	function changeType(value: string) {
		if (editing) return;
		backupType = value as BackupType;
		form = newForm(backupType);
		selectionMode = 'all';
		volumeNames = [];
		ignoreAnonymous = true;
		serverError = '';
		if (backupType === 'volume') void loadVolumeOptions();
	}

	function updateForm(values: Partial<BackupPolicyForm>) {
		form = { ...form, ...values };
		serverError = '';
	}

	function systemPayload(policy: SystemBackupPolicy): UpdateSystemBackupPolicy {
		return {
			id: policy.id,
			enabled: policy.enabled,
			schedule: policy.schedule,
			retentionCount: policy.retentionCount,
			localEnabled: policy.localEnabled,
			s3Enabled: policy.s3Enabled,
			s3DestinationId: policy.s3DestinationId ?? ''
		};
	}

	function volumePayload(policy: SystemVolumeBackupPolicy): UpdateSystemVolumeBackupPolicy {
		return {
			id: policy.id,
			enabled: policy.enabled,
			schedule: policy.schedule,
			retentionCount: policy.retentionCount,
			stopContainers: policy.stopContainers ?? false,
			localEnabled: policy.localEnabled,
			s3Enabled: policy.s3Enabled,
			s3DestinationId: policy.s3DestinationId ?? '',
			selectionMode: policy.selectionMode,
			volumeNames: policy.volumeNames,
			ignoreAnonymous: policy.ignoreAnonymous
		};
	}

	async function save() {
		if (saving || invalid) return;
		saving = true;
		serverError = '';
		try {
			const destination = backupPolicyDestinationValues(form.destination, form.s3DestinationId);
			if (backupType === 'system') {
				const current: UpdateSystemBackupPolicy = {
					id: policyId ?? '',
					enabled: form.enabled,
					schedule: form.schedule,
					retentionCount: Number(form.retentionCount),
					...destination
				};
				const existing = systemPolicies.map(systemPayload);
				const next = policyId ? existing.map((item) => (item.id === policyId ? current : item)) : [...existing, current];
				onSystemSaved((await systemBackupService.updatePolicies(next)).policies);
				toast.success(m.system_backups_policy_saved());
			} else {
				const current: UpdateSystemVolumeBackupPolicy = {
					id: policyId ?? '',
					enabled: form.enabled,
					schedule: form.schedule,
					retentionCount: Number(form.retentionCount),
					stopContainers: form.stopContainers ?? false,
					...destination,
					selectionMode,
					volumeNames,
					ignoreAnonymous
				};
				const existing = volumePolicies.map(volumePayload);
				const next = policyId ? existing.map((item) => (item.id === policyId ? current : item)) : [...existing, current];
				onVolumeSaved((await systemBackupService.updateSystemVolumeConfig(next)).policies);
				toast.success(m.system_volume_backups_saved());
			}
			open = false;
		} catch (error) {
			const message =
				error instanceof Error
					? error.message
					: backupType === 'system'
						? m.system_backups_policy_failed()
						: m.system_volume_backups_save_failed();
			if (/cron|schedule/i.test(message)) serverError = message;
			else toast.error(message);
		} finally {
			saving = false;
		}
	}

	async function remove() {
		if (!policyId || deleting) return;
		deleting = true;
		try {
			if (backupType === 'system') {
				onSystemSaved(
					(await systemBackupService.updatePolicies(systemPolicies.filter((item) => item.id !== policyId).map(systemPayload)))
						.policies
				);
				toast.success(m.system_backups_schedule_removed());
			} else {
				onVolumeSaved(
					(
						await systemBackupService.updateSystemVolumeConfig(
							volumePolicies.filter((item) => item.id !== policyId).map(volumePayload)
						)
					).policies
				);
				toast.success(m.system_volume_backups_schedule_removed());
			}
			open = false;
		} catch (error) {
			toast.error(
				error instanceof Error
					? error.message
					: backupType === 'system'
						? m.system_backups_policy_failed()
						: m.system_volume_backups_save_failed()
			);
		} finally {
			deleting = false;
		}
	}
</script>

<ResponsiveDialog
	bind:open
	title={editing ? m.jobs_edit_schedule() : m.system_backups_add_schedule()}
	description={m.system_backups_schedule_type_description()}
	contentClass="sm:max-w-[760px]"
>
	{#snippet children()}
		<div class="space-y-5 py-2">
			<SelectWithLabel
				id="system-backup-schedule-type"
				value={backupType}
				onValueChange={changeType}
				label={m.backups_backup_type()}
				description={m.backups_backup_type_description()}
				options={typeOptions}
				disabled={editing}
			/>

			<BackupPolicyFields
				idPrefix={`${backupType}-backup-policy`}
				{form}
				{destinations}
				{scheduleError}
				{retentionError}
				{destinationError}
				{enabledError}
				enabledDescription={backupType === 'system'
					? m.system_backups_enabled_description()
					: m.system_volume_backups_enabled_description()}
				schedulePlaceholder={backupType === 'system' ? '0 0 3 * * *' : '0 0 2 * * *'}
				showStopContainers={backupType === 'volume'}
				onChange={updateForm}
			/>

			{#if backupType === 'volume'}
				<SystemVolumeScopeFields
					idPrefix="system-volume-backup-schedule"
					{selectionMode}
					{volumeNames}
					{ignoreAnonymous}
					{options}
					loading={optionsLoading}
					onChange={(values) => {
						selectionMode = values.selectionMode ?? selectionMode;
						volumeNames = values.volumeNames ?? volumeNames;
						ignoreAnonymous = values.ignoreAnonymous ?? ignoreAnonymous;
					}}
				/>
			{/if}
		</div>
	{/snippet}
	{#snippet footer()}
		{#if editing}
			<ArcaneButton
				action="remove"
				customLabel={m.backups_remove_schedule()}
				onclick={remove}
				loading={deleting}
				disabled={saving || deleting}
			/>
		{/if}
		<ArcaneButton action="cancel" onclick={() => (open = false)} disabled={saving || deleting} />
		<ArcaneButton action="save" onclick={save} loading={saving} disabled={saving || deleting || invalid} />
	{/snippet}
</ResponsiveDialog>
