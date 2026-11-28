<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';

	import BackupPolicyDialog from '#lib/components/backup-policy-dialog.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { AlertIcon } from '#lib/icons/index.js';
	import * as m from '#lib/paraglide/messages.js';
	import { projectBackupOptionsQuery } from '#lib/query/backups.js';
	import { systemBackupService } from '#lib/services/system-backup-service.js';
	import type {
		UpdateBackupPolicy,
		S3Destination,
		SystemBackupPolicy,
		SystemVolumeBackupOption,
		SystemVolumeBackupPolicy,
		SystemVolumeSelectionMode,
		UpdateSystemVolumeBackupPolicy
	} from '#lib/types/backup.js';
	import type { GlobalProjectBackupPolicy, UpdateGlobalProjectBackupPolicy } from '#lib/types/project.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { backupPolicyUpdateFromPolicy } from '#lib/utils/backups.js';

	import BackupScopeFields, { parseProjectVolumeValue, projectVolumeValue } from './backup-scope-fields.svelte';

	type BackupType = 'system' | 'project' | 'volume';

	let {
		open = $bindable(),
		initialType,
		policyId,
		systemPolicies,
		projectPolicies,
		volumePolicies,
		recoveryKeyStored,
		destinations,
		volumeOptions,
		volumeOptionsLoading,
		onLoadVolumeOptions,
		onSystemSaved,
		onProjectSaved,
		onVolumeSaved
	}: {
		open: boolean;
		initialType: BackupType;
		policyId?: string;
		systemPolicies: SystemBackupPolicy[];
		projectPolicies: GlobalProjectBackupPolicy[];
		volumePolicies: SystemVolumeBackupPolicy[];
		recoveryKeyStored: boolean;
		destinations: S3Destination[];
		volumeOptions: SystemVolumeBackupOption[];
		volumeOptionsLoading: boolean;
		onLoadVolumeOptions: () => Promise<void>;
		onSystemSaved: (policies: SystemBackupPolicy[]) => void;
		onProjectSaved: (policies: GlobalProjectBackupPolicy[]) => void;
		onVolumeSaved: (policies: SystemVolumeBackupPolicy[]) => void;
	} = $props();

	const editing = untrack(() => Boolean(policyId));
	let backupType = $state<BackupType>(untrack(() => initialType));
	const initialProjectPolicy = untrack(() =>
		initialType === 'project' ? projectPolicies.find((item) => item.id === policyId) : undefined
	);
	const initialScope = untrack(() => {
		if (initialType === 'project') {
			return {
				selectionMode: initialProjectPolicy?.selectionMode,
				selected: initialProjectPolicy?.projectIds,
				ignoreAnonymous: true
			};
		}
		const policy = volumePolicies.find((item) => item.id === policyId);
		return { selectionMode: policy?.selectionMode, selected: policy?.volumeNames, ignoreAnonymous: policy?.ignoreAnonymous };
	});
	let selectionMode = $state<SystemVolumeSelectionMode>(initialScope.selectionMode ?? 'all');
	let selected = $state<string[]>([...(initialScope.selected ?? [])]);
	let ignoreAnonymous = $state(initialScope.ignoreAnonymous ?? true);
	let volumeSelectionMode = $state<SystemVolumeSelectionMode>(initialProjectPolicy?.volumeSelectionMode ?? 'all');
	let selectedVolumes = $state<string[]>((initialProjectPolicy?.volumes ?? []).map(projectVolumeValue));
	const projectOptionsQuery = createQuery(() => ({ ...projectBackupOptionsQuery(), enabled: backupType === 'project' }));
	const projectVolumeOptions = $derived((projectOptionsQuery.data ?? []).filter((project) => projectSelected(project.id)));
	const typeOptions = $derived([
		{ label: m.system(), value: 'system', description: m.arcane_only_backup_description() },
		{ label: m.project(), value: 'project', description: m.back_up_selected_projects() },
		{ label: m.resource_volume_cap(), value: 'volume', description: m.system_backups_type_volume_description() }
	]);

	function changeType(value: string) {
		if (editing) return;
		backupType = value as BackupType;
		if (backupType === 'volume') void onLoadVolumeOptions();
		selectionMode = 'all';
		selected = [];
		ignoreAnonymous = true;
		volumeSelectionMode = 'all';
		selectedVolumes = [];
	}

	function projectSelected(projectId: string): boolean {
		switch (selectionMode) {
			case 'allowlist':
				return selected.includes(projectId);
			case 'blocklist':
				return !selected.includes(projectId);
			default:
				return true;
		}
	}

	function volumePayload(policy: SystemVolumeBackupPolicy): UpdateSystemVolumeBackupPolicy {
		return {
			...backupPolicyUpdateFromPolicy(policy, true),
			selectionMode: policy.selectionMode,
			volumeNames: policy.volumeNames,
			ignoreAnonymous: policy.ignoreAnonymous
		};
	}

	function extendVolumeUpdate(update: UpdateBackupPolicy): UpdateSystemVolumeBackupPolicy {
		return {
			...update,
			stopContainers: update.stopContainers ?? false,
			selectionMode,
			volumeNames: selected,
			ignoreAnonymous
		};
	}

	function projectPayload(policy: GlobalProjectBackupPolicy): UpdateGlobalProjectBackupPolicy {
		return {
			...backupPolicyUpdateFromPolicy(policy, true),
			selectionMode: policy.selectionMode,
			projectIds: policy.projectIds,
			volumeSelectionMode: policy.volumeSelectionMode,
			volumes: policy.volumes
		};
	}

	function extendProjectUpdate(update: UpdateBackupPolicy): UpdateGlobalProjectBackupPolicy {
		return {
			...update,
			selectionMode,
			projectIds: selected,
			volumeSelectionMode,
			volumes: selectedVolumes.map(parseProjectVolumeValue)
		};
	}

	async function updateSystemPolicies(policies: UpdateBackupPolicy[]) {
		return (await systemBackupService.updatePolicies(policies)).policies;
	}

	async function updateProjectPolicies(policies: UpdateGlobalProjectBackupPolicy[]) {
		return (await systemBackupService.updateProjectConfig(policies)).policies;
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

{#snippet Scope()}
	{#if backupType === 'project' && projectOptionsQuery.error}
		<Alert.Root variant="destructive" size="sm">
			<AlertIcon class="size-4" />
			<Alert.Description>{extractApiErrorMessage(projectOptionsQuery.error)}</Alert.Description>
		</Alert.Root>
	{/if}
	<BackupScopeFields
		idPrefix={`system-${backupType}-backup-schedule`}
		kind={backupType === 'project' ? 'project' : 'volume'}
		{selectionMode}
		{selected}
		{ignoreAnonymous}
		options={backupType === 'project' ? (projectOptionsQuery.data ?? []) : volumeOptions}
		loading={backupType === 'project' ? projectOptionsQuery.isFetching : volumeOptionsLoading}
		onChange={(values) => {
			selectionMode = values.selectionMode ?? selectionMode;
			selected = values.selected ?? selected;
			ignoreAnonymous = values.ignoreAnonymous ?? ignoreAnonymous;
		}}
	/>
	{#if backupType === 'project'}
		<BackupScopeFields
			idPrefix="system-project-volume-backup-schedule"
			kind="projectVolume"
			selectionMode={volumeSelectionMode}
			selected={selectedVolumes}
			options={projectVolumeOptions}
			loading={projectOptionsQuery.isFetching}
			onChange={(values) => {
				volumeSelectionMode = values.selectionMode ?? volumeSelectionMode;
				selectedVolumes = values.selected ?? selectedVolumes;
			}}
		/>
	{/if}
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
		defaultSchedule="0 3 * * *"
		defaultEnabled={recoveryKeyStored}
		{destinations}
		beforeFields={TypeField}
		updatePolicies={updateSystemPolicies}
		messages={{
			saved: m.system_backups_policy_saved(),
			saveFailed: m.system_backups_policy_failed(),
			removed: m.system_backups_schedule_removed()
		}}
		onSaved={onSystemSaved}
		contentClass="sm:max-w-190"
	/>
{:else if backupType === 'project'}
	<BackupPolicyDialog
		bind:open
		idPrefix="global-project-backup-policy"
		policies={projectPolicies}
		{policyId}
		addTitle={m.system_backups_add_schedule()}
		description={m.system_backups_schedule_type_description()}
		enabledDescription={m.back_up_selected_projects_on_schedule()}
		defaultSchedule="0 2 * * *"
		showStopContainers
		stopContainersDescription={m.stop_each_project_during_snapshot()}
		retentionDescription={m.keep_newest_successful_backups_per_project()}
		defaultStopContainers
		{destinations}
		beforeFields={TypeField}
		afterFields={Scope}
		policyPayload={projectPayload}
		extendUpdate={extendProjectUpdate}
		updatePolicies={updateProjectPolicies}
		messages={{
			saved: m.backup_schedules_saved(),
			saveFailed: m.failed_to_save_backup_schedules(),
			removed: m.backup_schedule_removed()
		}}
		onSaved={onProjectSaved}
		contentClass="sm:max-w-190"
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
		defaultSchedule="0 2 * * *"
		showStopContainers
		{destinations}
		beforeFields={TypeField}
		afterFields={Scope}
		policyPayload={volumePayload}
		extendUpdate={extendVolumeUpdate}
		updatePolicies={updateVolumePolicies}
		messages={{
			saved: m.system_volume_backups_saved(),
			saveFailed: m.system_volume_backups_save_failed(),
			removed: m.system_volume_backups_schedule_removed()
		}}
		onSaved={onVolumeSaved}
		contentClass="sm:max-w-190"
	/>
{/if}
