<script lang="ts">
	import { untrack } from 'svelte';
	import BackupPolicyDialog from '#lib/components/backup-policy-dialog.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import { systemBackupService } from '#lib/services/system-backup-service.js';
	import type { BackupPolicyUpdate } from '#lib/types/backup.js';
	import type { S3Destination } from '#lib/types/s3-destination.js';
	import type {
		SystemBackupPolicy,
		SystemVolumeBackupOption,
		SystemVolumeBackupPolicy,
		SystemVolumeBackupSelectionMode,
		UpdateSystemBackupPolicy,
		UpdateSystemVolumeBackupPolicy
	} from '#lib/types/system-backup.js';
	import { backupPolicyUpdateFromPolicy } from '#lib/utils/backups.js';
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
		volumeOptions,
		volumeOptionsLoading,
		onLoadVolumeOptions,
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
		volumeOptions: SystemVolumeBackupOption[];
		volumeOptionsLoading: boolean;
		onLoadVolumeOptions: () => Promise<void>;
		onSystemSaved: (policies: SystemBackupPolicy[]) => void;
		onVolumeSaved: (policies: SystemVolumeBackupPolicy[]) => void;
	} = $props();

	const editing = untrack(() => Boolean(policyId));
	let backupType = $state<BackupType>(untrack(() => initialType));
	let selectionMode = $state<SystemVolumeBackupSelectionMode>('all');
	let volumeNames = $state<string[]>([]);
	let ignoreAnonymous = $state(true);
	const typeOptions = $derived([
		{ label: m.system(), value: 'system', description: m.system_backups_type_system_description() },
		{ label: m.resource_volume_cap(), value: 'volume', description: m.system_backups_type_volume_description() }
	]);

	$effect(() => {
		if (backupType === 'volume') void onLoadVolumeOptions();
	});

	function changeType(value: string) {
		if (editing) return;
		backupType = value as BackupType;
		selectionMode = 'all';
		volumeNames = [];
		ignoreAnonymous = true;
	}

	function volumePayload(policy: SystemVolumeBackupPolicy): UpdateSystemVolumeBackupPolicy {
		return {
			...backupPolicyUpdateFromPolicy(policy, true),
			selectionMode: policy.selectionMode,
			volumeNames: policy.volumeNames,
			ignoreAnonymous: policy.ignoreAnonymous
		};
	}

	function extendVolumeUpdate(update: BackupPolicyUpdate): UpdateSystemVolumeBackupPolicy {
		return {
			...update,
			stopContainers: update.stopContainers ?? false,
			selectionMode,
			volumeNames,
			ignoreAnonymous
		};
	}

	function resetVolumeScope(policy?: SystemVolumeBackupPolicy) {
		selectionMode = policy?.selectionMode ?? 'all';
		volumeNames = policy ? [...policy.volumeNames] : [];
		ignoreAnonymous = policy?.ignoreAnonymous ?? true;
	}

	async function updateSystemPolicies(policies: UpdateSystemBackupPolicy[]) {
		return (await systemBackupService.updatePolicies(policies)).policies;
	}

	async function updateVolumePolicies(policies: UpdateSystemVolumeBackupPolicy[]) {
		return (await systemBackupService.updateSystemVolumeConfig(policies)).policies;
	}
</script>

{#snippet TypeField()}
	<SelectWithLabel
		id="system-backup-schedule-type"
		value={backupType}
		onValueChange={changeType}
		label={m.backups_backup_type()}
		description={m.backups_backup_type_description()}
		options={typeOptions}
		disabled={editing}
	/>
{/snippet}

{#snippet VolumeScope()}
	<SystemVolumeScopeFields
		idPrefix="system-volume-backup-schedule"
		{selectionMode}
		{volumeNames}
		{ignoreAnonymous}
		options={volumeOptions}
		loading={volumeOptionsLoading}
		onChange={(values) => {
			selectionMode = values.selectionMode ?? selectionMode;
			volumeNames = values.volumeNames ?? volumeNames;
			ignoreAnonymous = values.ignoreAnonymous ?? ignoreAnonymous;
		}}
	/>
{/snippet}

{#if backupType === 'system'}
	<BackupPolicyDialog
		bind:open
		idPrefix="system-backup-policy"
		policies={systemPolicies}
		{policyId}
		addTitle={m.system_backups_add_schedule()}
		description={m.system_backups_schedule_type_description()}
		enabledDescription={m.system_backups_enabled_description()}
		enabledError={recoveryKeyStored ? null : m.system_backups_recovery_key_schedule_required()}
		defaultSchedule="0 0 3 * * *"
		defaultEnabled={recoveryKeyStored}
		{destinations}
		resetKey={backupType}
		beforeFields={TypeField}
		updatePolicies={updateSystemPolicies}
		messages={{
			saved: m.system_backups_policy_saved(),
			saveFailed: m.system_backups_policy_failed(),
			removed: m.system_backups_schedule_removed()
		}}
		onSaved={onSystemSaved}
		contentClass="sm:max-w-[760px]"
	/>
{:else}
	<BackupPolicyDialog
		bind:open
		idPrefix="system-volume-backup-policy"
		policies={volumePolicies}
		{policyId}
		addTitle={m.system_backups_add_schedule()}
		description={m.system_backups_schedule_type_description()}
		enabledDescription={m.system_volume_backups_enabled_description()}
		defaultSchedule="0 0 2 * * *"
		showStopContainers
		{destinations}
		resetKey={backupType}
		beforeFields={TypeField}
		afterFields={VolumeScope}
		policyPayload={volumePayload}
		extendUpdate={extendVolumeUpdate}
		updatePolicies={updateVolumePolicies}
		messages={{
			saved: m.system_volume_backups_saved(),
			saveFailed: m.system_volume_backups_save_failed(),
			removed: m.system_volume_backups_schedule_removed()
		}}
		onSaved={onVolumeSaved}
		onReset={resetVolumeScope}
		contentClass="sm:max-w-[760px]"
	/>
{/if}
