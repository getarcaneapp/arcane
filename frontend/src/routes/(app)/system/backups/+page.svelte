<script lang="ts">
	import { goto, afterNavigate } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { onMount, onDestroy, untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	import type { ActionButton, ActionButtonMenuItem } from '#lib/components/action-button-group/types.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import BackupFilePicker from '#lib/components/backup-file-picker.svelte';
	import BackupPolicyCard from '#lib/components/backup-policy-card.svelte';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import ProjectRestoreAsNewDialog from '#lib/components/dialogs/project-restore-as-new-dialog.svelte';
	import LabeledSwitch from '#lib/components/form/labeled-switch.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import TextInputWithLabel from '#lib/components/form/text-input-with-label.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import { EmptyState } from '#lib/components/states/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { CopyButton } from '#lib/components/ui/copy-button/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog/index.js';
	import { useBackupActivity } from '#lib/hooks/use-backup-activity.svelte.js';
	import {
		AddIcon,
		AlertIcon,
		BackupIcon,
		CloudStorageIcon,
		InfoIcon,
		LockIcon,
		ResetIcon,
		UploadIcon
	} from '#lib/icons/index.js';
	import { SettingsPageLayout } from '#lib/layouts/index.js';
	import * as m from '#lib/paraglide/messages.js';
	import { projectBackupConfigQuery, systemBackupHistoryQuery } from '#lib/query/backups.js';
	import { systemVolumeBackupOptionsQuery } from '#lib/query/backups.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { projectService } from '#lib/services/project-service.js';
	import { systemBackupService } from '#lib/services/system-backup-service.js';
	import { volumeBackupService } from '#lib/services/volume-backup-service.js';
	import { activityStore } from '#lib/stores/activity.store.svelte.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import { environmentStore, isLocalDockerDisabled, LOCAL_DOCKER_ENVIRONMENT_ID } from '#lib/stores/environment.store.svelte.js';
	import type {
		CreateSystemBackupRequest,
		RunSystemVolumeBackupsRequest,
		SystemVolumeBackupHistoryEntry,
		SystemBackupDestination,
		SystemVolumeBackupOption,
		SystemVolumeSelectionMode
	} from '#lib/types/backup.js';
	import type { ProjectBackup } from '#lib/types/project.js';
	import { GLOBAL_SCOPE } from '#lib/types/role.js';
	import type { BackupFileProvider, BackupFileRootLoadState } from '#lib/types/ui/backup.js';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast.js';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasAnyPermission, hasPermission, isGlobalAdmin } from '#lib/utils/auth.js';
	import {
		backupDestinationOptions,
		backupPolicyDestinationDisplay,
		runAutomaticBackupDiscovery,
		s3DestinationOptions
	} from '#lib/utils/backups.js';
	import type { EnvironmentRow } from '#lib/utils/environments.js';
	import { ResourceListPageState } from '#lib/utils/resource-list-page.svelte.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import BackupScopeFields from './components/backup-scope-fields.svelte';
	import SystemBackupScheduleDialog from './components/system-backup-schedule-dialog.svelte';
	import SystemBackupTable from './components/system-backup-table.svelte';

	type ScheduleType = 'system' | 'project' | 'volume';

	let { data } = $props();
	const queryClient = useQueryClient();
	const pageState = new ResourceListPageState(untrack(() => data.requestOptions));
	const backupsQuery = createQuery(() => ({
		...systemBackupHistoryQuery(pageState.scope, pageState.requestOptions),
		placeholderData: (previous) => previous
	}));
	const backups = $derived(backupsQuery.data ?? data.backups);
	let policyCollection = $derived(data.policyCollection);
	// Observed rather than taken from the load: a global run can disable a schedule's S3 destination, and saving sends
	// the whole list, so a stale copy would turn it back on.
	const projectPolicyQuery = createQuery(() => ({
		...projectBackupConfigQuery(),
		enabled: hasPermission('system-backups:read', GLOBAL_SCOPE)
	}));
	const projectPolicyCollection = $derived(projectPolicyQuery.data ?? data.projectPolicyCollection);
	let systemVolumePolicyCollection = $derived(data.systemVolumePolicyCollection);
	const systemVolumeOptionsQuery = createQuery(() => ({
		...systemVolumeBackupOptionsQuery(),
		enabled: false
	}));
	const systemVolumeOptions = $derived<SystemVolumeBackupOption[]>(systemVolumeOptionsQuery.data ?? []);
	const systemVolumeOptionsLoading = $derived(systemVolumeOptionsQuery.isFetching);
	let systemVolumeOptionsLoaded = false;
	let scheduleOpen = $state(false);
	let scheduleSession = $state(0);
	let scheduleType = $state<ScheduleType>('system');
	let runningPolicyId = $state<string | null>(null);
	let editingScheduleId = $state<string | undefined>();
	let keyOpen = $state(false);
	let importKeyOpen = $state(false);
	let importKeyInput = $state('');
	let importingKey = $state(false);
	let actionOpen = $state(false);
	let action = $state<'create' | 'restore' | 'upload' | 'delete' | 'discover'>('create');
	let selected = $state<EnvironmentRow<SystemVolumeBackupHistoryEntry> | null>(null);
	let backupType = $state<'system' | 'volume'>('system');
	let backupConfiguration = $state('custom');
	let destination = $state<SystemBackupDestination>('local');
	let s3DestinationId = $state('');
	let stopContainers = $state(false);
	let selectionMode = $state<SystemVolumeSelectionMode>('all');
	let volumeNames = $state<string[]>([]);
	let ignoreAnonymous = $state(true);
	let recoveryKey = $state('');
	let newRecoveryKey = $state('');
	let loading = $state(false);
	let generatingKey = $state(false);
	let restoreFilesOpen = $state(false);
	let restoreFilesTarget = $state<EnvironmentRow<SystemVolumeBackupHistoryEntry> | null>(null);
	let restoreFilesRecoveryKey = $state('');
	let restoreFilesProvider = $state<BackupFileProvider | null>(null);
	let restoreFilesSelectedPaths = $state<string[]>([]);
	let restoreFilesSelectAll = $state(false);
	let restoreFilesSearch = $state('');
	let restoreFilesLoaded = $state(false);
	let restoringFiles = $state(false);
	let restoreAsNewOpen = $state(false);
	let restoreAsNewBackup = $state<ProjectBackup | null>(null);
	let restoreAsNewEnvironmentId = $state<string>(LOCAL_DOCKER_ENVIRONMENT_ID);
	const isReadOnly = $derived(settingsStore.readOnly);
	const unmanagedBackupRetentionDays = $derived(settingsStore.current?.unmanagedBackupRetentionDays);
	// System backups live in a local Docker volume, so they need the manager's own Docker.
	// The policies response carries the flag for users whose environment list lacks the manager; only an explicit false counts.
	const localDockerDisabled = $derived(
		policyCollection.localDockerEnabled === false || isLocalDockerDisabled(environmentStore.getLocalEnvironment())
	);
	const canCreateBackups = $derived(hasPermission('system-backups:create', GLOBAL_SCOPE) && !localDockerDisabled);
	// Saving sends a whole list of schedules, which only loads with system-backups:read.
	const canUpdateBackupPolicies = $derived(
		hasPermission('system-backups:update', GLOBAL_SCOPE) &&
			hasPermission('system-backups:read', GLOBAL_SCOPE) &&
			!localDockerDisabled
	);
	const canManageRecoveryKey = $derived(isGlobalAdmin());
	// Discovery restores snapshots through the manager's Docker, even for S3 destinations.
	const canDiscoverBackups = $derived(isGlobalAdmin() && !localDockerDisabled);
	const destinationOptions = $derived(backupDestinationOptions(data.destinations.length > 0));
	const s3Options = $derived(s3DestinationOptions(data.destinations));
	const backupTypeOptions = $derived([
		{ label: m.system(), value: 'system', description: m.arcane_only_backup_description() },
		{ label: m.resource_volume_cap(), value: 'volume', description: m.system_backups_type_volume_description() }
	]);
	const configurationOptions = $derived([
		{
			label: m.system_backups_custom_configuration(),
			value: 'custom',
			description: m.system_backups_custom_configuration_description()
		},
		...(backupType === 'system' ? policyCollection.policies : systemVolumePolicyCollection.policies).map((item) => ({
			label: item.schedule,
			value: item.id,
			description: backupPolicyDestinationDisplay(item)
		}))
	]);
	// Restore and discover may target backups keyed by another instance, so
	// they accept a typed (previously generated) key. Everything else only
	// ever uses this instance's stored key.
	const actionNeedsTypedKey = $derived(action === 'restore' || action === 'discover');
	const selectedRemote = $derived(selected !== null && selected.environmentId !== LOCAL_DOCKER_ENVIRONMENT_ID);
	const selectedProject = $derived(selected?.resourceType === 'project');
	const restoreFilesRemote = $derived(
		restoreFilesTarget !== null && restoreFilesTarget.environmentId !== LOCAL_DOCKER_ENVIRONMENT_ID
	);
	const restoreFilesKeyInvalid = $derived(
		!restoreFilesRemote && !policyCollection.recoveryKeyStored && !restoreFilesRecoveryKey.trim()
	);
	const importKeyInvalid = $derived(!importKeyInput.trim());
	const actionKeyInvalid = $derived.by(() => {
		if (action === 'create' && backupType === 'volume') return false;
		if (selectedRemote) return false;
		if (actionNeedsTypedKey) return !policyCollection.recoveryKeyStored && !recoveryKey.trim();
		// Deleting is always allowed; the backend only asks for the key when
		// the run still has snapshots to forget.
		if (action === 'delete') return false;
		return !policyCollection.recoveryKeyStored;
	});
	const destinationError = $derived.by(() => {
		const needsS3 = (action === 'create' && destination !== 'local') || action === 'upload' || action === 'discover';
		if (backupConfiguration === 'custom' && needsS3 && !s3DestinationId) {
			return m.volume_backup_s3_destination_required();
		}
		return '';
	});
	const invalid = $derived(Boolean(actionKeyInvalid || destinationError));

	function openSchedule(type: ScheduleType = 'system', id?: string) {
		scheduleSession += 1;
		if (type === 'volume') void loadSystemVolumeOptions();
		scheduleType = type;
		editingScheduleId = id;
		scheduleOpen = true;
	}

	// Generating persists the key in the same step; the dialog only ever shows
	// an already-saved key, with one job: get the user to store it elsewhere.
	async function generateAndStoreRecoveryKey() {
		newRecoveryKey = '';
		generatingKey = true;
		try {
			const generated = await tryCatch(systemBackupService.generateRecoveryKey());
			if (generated.error !== null) {
				toast.error(generated.error.message || m.system_backups_recovery_key_save_failed());
				return;
			}
			const stored = await tryCatch(systemBackupService.setRecoveryKey(generated.data.recoveryKey));
			if (stored.error !== null) {
				toast.error(stored.error.message || m.system_backups_recovery_key_save_failed());
				return;
			}
			newRecoveryKey = generated.data.recoveryKey;
			policyCollection = { ...policyCollection, recoveryKeyStored: true };
			void queryClient.invalidateQueries({ queryKey: queryKeys.systemBackups.policies() });
			void discoverStoredBackups({ local: true });
			keyOpen = true;
		} finally {
			generatingKey = false;
		}
	}

	// Rotating a stored key invalidates repositories keyed by the old one, so
	// it must be confirmed; first-time setup has nothing to lose.
	function openRecoveryKey() {
		if (!policyCollection.recoveryKeyStored) {
			void generateAndStoreRecoveryKey();
			return;
		}
		openConfirmDialog({
			title: m.system_backups_reset_recovery_key(),
			message: m.system_backups_reset_recovery_key_confirm_message(),
			confirm: {
				label: m.system_backups_reset_recovery_key(),
				destructive: true,
				action: () => generateAndStoreRecoveryKey()
			}
		});
	}

	function openImportKey() {
		importKeyInput = '';
		importKeyOpen = true;
	}

	async function importRecoveryKey() {
		if (importingKey || importKeyInvalid) return;
		importingKey = true;
		try {
			const stored = await tryCatch(systemBackupService.setRecoveryKey(importKeyInput.trim()));
			if (stored.error !== null) {
				toast.error(stored.error.message || m.system_backups_recovery_key_save_failed());
				return;
			}
			policyCollection = { ...policyCollection, recoveryKeyStored: true };
			void queryClient.invalidateQueries({ queryKey: queryKeys.systemBackups.policies() });
			void discoverStoredBackups({ local: true });
			importKeyOpen = false;
			toast.success(m.system_backups_recovery_key_saved());
		} finally {
			importingKey = false;
		}
	}

	function openAction(next: typeof action, backup: EnvironmentRow<SystemVolumeBackupHistoryEntry> | null = null) {
		action = next;
		selected = backup;
		backupType = 'system';
		backupConfiguration = 'custom';
		destination = 'local';
		stopContainers = false;
		selectionMode = 'all';
		volumeNames = [];
		ignoreAnonymous = true;
		s3DestinationId =
			backup?.s3DestinationId ||
			policyCollection.policies.find((item) => item.s3DestinationId)?.s3DestinationId ||
			data.destinations[0]?.id ||
			'';
		recoveryKey = '';
		actionOpen = true;
	}

	// Replacing the provider remounts the picker; its root-load callback is the
	// only thing that marks the dialog ready to restore.
	function resetRestoreFilesPicker() {
		restoreFilesProvider = null;
		restoreFilesLoaded = false;
		restoreFilesSelectedPaths = [];
		restoreFilesSelectAll = false;
		restoreFilesSearch = '';
	}

	async function openRestoreFiles(backup: EnvironmentRow<SystemVolumeBackupHistoryEntry>) {
		restoreFilesTarget = backup;
		restoreFilesRecoveryKey = '';
		resetRestoreFilesPicker();
		restoreFilesOpen = true;
		if (policyCollection.recoveryKeyStored || backup.environmentId !== LOCAL_DOCKER_ENVIRONMENT_ID) loadRestoreFiles();
	}

	function closeRestoreFiles() {
		restoreFilesOpen = false;
		resetRestoreFilesPicker();
	}

	function updateRestoreFilesRecoveryKey(value: string) {
		restoreFilesRecoveryKey = value;
		resetRestoreFilesPicker();
	}

	function loadRestoreFiles() {
		if (!restoreFilesTarget || restoreFilesKeyInvalid) return;
		resetRestoreFilesPicker();
		const environmentID = restoreFilesTarget.environmentId;
		const backupID = restoreFilesTarget.id;
		const recoveryKey = restoreFilesRecoveryKey.trim();
		restoreFilesProvider = {
			browse: (request) => systemBackupService.browseFiles(environmentID, backupID, recoveryKey, request)
		};
	}

	function updateRestoreFilesRootLoad(state: BackupFileRootLoadState) {
		restoreFilesLoaded = state === 'ready';
	}

	async function restoreSelectedFiles() {
		if (
			!restoreFilesTarget ||
			!restoreFilesLoaded ||
			(!restoreFilesSelectAll && restoreFilesSelectedPaths.length === 0) ||
			restoringFiles
		)
			return;
		restoringFiles = true;
		try {
			const environmentId = restoreFilesTarget.environmentId;
			const restored = await tryCatch(
				systemBackupService.restoreFiles(environmentId, restoreFilesTarget.id, restoreFilesRecoveryKey.trim(), {
					paths: restoreFilesSelectedPaths,
					selectAll: restoreFilesSelectAll,
					search: restoreFilesSelectAll ? restoreFilesSearch.trim() : undefined
				})
			);
			if (restored.error !== null) {
				toast.error(restored.error.message || m.system_backups_restore_files_failed());
				return;
			}
			const projectQueryKey = queryKeys.projects.environment(environmentId);
			await queryClient.cancelQueries({ queryKey: projectQueryKey });
			queryClient.removeQueries({ queryKey: projectQueryKey });
			toast.success(m.system_backups_restore_selection_success(), activityToastOptions(extractActivityId(restored.data)));
			closeRestoreFiles();
			await refresh();
		} finally {
			restoringFiles = false;
		}
	}

	function dialogTitle() {
		if (action === 'restore') return m.system_backups_restore_title();
		if (action === 'upload') return m.system_backups_upload_title();
		if (action === 'delete' && selectedProject) return m.common_delete_title({ resource: m.project_backup() });
		if (action === 'delete') return m.system_backups_delete_title();
		if (action === 'discover') return m.system_backups_discover_title();
		return m.volumes_backup_create();
	}
	function dialogDescription() {
		if (action === 'restore') return m.system_backups_restore_description();
		if (action === 'delete' && selectedProject) return m.remove_this_backup();
		if (action === 'delete') return m.system_backups_delete_description();
		if (action === 'discover') return m.system_backups_discover_description();
		return backupType === 'volume' ? m.system_volume_backups_description() : m.system_backups_dialog_description();
	}
	function actionFailedMessage() {
		if (action === 'restore') return m.system_backups_restore_failed();
		if (action === 'delete' && selectedProject) return m.common_delete_failed({ resource: m.project_backup() });
		if (action === 'delete') return m.system_backups_delete_failed();
		if (action === 'upload') return m.backups_upload_s3_failed();
		if (action === 'discover') return m.system_backups_discover_failed();
		return backupType === 'volume' ? m.system_volume_backups_run_failed() : m.system_backups_create_failed();
	}
	const submitButtonAction = {
		create: 'create',
		restore: 'confirm',
		upload: 'save',
		delete: 'remove',
		discover: 'save'
	} as const;
	async function refresh() {
		await queryClient.invalidateQueries({ queryKey: queryKeys.systemBackups.historyPrefix() });
	}

	async function loadSystemVolumeOptions() {
		if (systemVolumeOptionsQuery.isFetching || systemVolumeOptionsLoaded) return;
		const result = await systemVolumeOptionsQuery.refetch();
		if (result.isError) {
			toast.error(result.error.message || m.system_volume_backups_options_failed());
			return;
		}
		systemVolumeOptionsLoaded = true;
	}

	function changeBackupType(value: string) {
		backupType = value as 'system' | 'volume';
		backupConfiguration = 'custom';
		destination = 'local';
		s3DestinationId = data.destinations[0]?.id ?? '';
		if (backupType === 'volume') void loadSystemVolumeOptions();
	}

	async function openVolumeBackups(backup: EnvironmentRow<SystemVolumeBackupHistoryEntry>) {
		await goto(
			resolve(`/(app)/volumes/[volumeName]?tab=backups&env=${encodeURIComponent(backup.environmentId)}`, {
				volumeName: encodeURIComponent(backup.resourceName)
			})
		);
	}

	async function openProjectBackups(backup: EnvironmentRow<SystemVolumeBackupHistoryEntry>) {
		await goto(
			resolve(`/(app)/projects/[projectId]?tab=backups&env=${encodeURIComponent(backup.environmentId)}`, {
				projectId: encodeURIComponent(backup.resourceId ?? '')
			})
		);
	}

	// The history row lacks the backup's volumes, which the dialog offers to restore.
	async function openRestoreAsNew(backup: EnvironmentRow<SystemVolumeBackupHistoryEntry>) {
		await handleApiResultWithCallbacks({
			result: await tryCatch(projectService.getBackup(backup.resourceId ?? '', backup.id, backup.environmentId)),
			message: m.failed_to_restore_backup(),
			onSuccess: (loaded) => {
				restoreAsNewBackup = loaded;
				restoreAsNewEnvironmentId = backup.environmentId;
				restoreAsNewOpen = true;
			}
		});
	}

	let autoDiscovered = false;
	let mounted = false;
	// Page loads scan S3 destinations; a new or imported recovery key also scans local storage, which a reinstall leaves unrecorded.
	async function discoverStoredBackups({ local = false } = {}) {
		if (!mounted || !policyCollection.recoveryKeyStored || !canDiscoverBackups) return;
		if (!local && (autoDiscovered || !data.destinations.length)) return;
		autoDiscovered = true;
		const found = await runAutomaticBackupDiscovery(data.destinations, { local });
		if (mounted && found) await refresh();
	}
	onMount(() => {
		mounted = true;
	});
	$effect(() => {
		if (!canDiscoverBackups) return;
		untrack(() => {
			void discoverStoredBackups();
		});
	});
	onDestroy(() => {
		mounted = false;
	});
	afterNavigate(() => {
		void discoverStoredBackups();
	});

	function openVolumeRestore(backup: EnvironmentRow<SystemVolumeBackupHistoryEntry>) {
		openConfirmDialog({
			title: m.volumes_backup_restore_title(),
			message: m.volumes_backup_restore_message({ volumeName: backup.resourceName }),
			confirm: {
				label: m.volumes_backups_restore(),
				action: async () => {
					const restored = await tryCatch(
						volumeBackupService.restoreBackup(backup.resourceName, backup.id, backup.environmentId)
					);
					if (restored.error !== null) {
						toast.error(restored.error.message || m.common_failed());
						return;
					}
					toast.success(m.volumes_backup_restore_success(), activityToastOptions(extractActivityId(restored.data)));
				}
			}
		});
	}

	const backupActivity = useBackupActivity(
		() => '0',
		(activity) =>
			activity.metadata?.['action'] === 'create_system_backup' ||
			activity.metadata?.['action'] === 'scheduled_system_backup' ||
			activity.metadata?.['action'] === 'run_system_volume_backups',
		() => queryClient.invalidateQueries({ queryKey: queryKeys.systemBackups.historyPrefix() }, { throwOnError: true })
	);
	// Project runs take each project's own turn, so they never hold up a system or volume backup.
	const projectActivity = useBackupActivity(
		() => '0',
		(activity) =>
			activity.metadata?.['action'] === 'run_project_backups' || activity.metadata?.['action'] === 'scheduled_project_backups',
		// A run may disable a schedule's missing S3 destination, so the schedules reload with the history.
		() =>
			Promise.all([
				queryClient.invalidateQueries({ queryKey: queryKeys.systemBackups.historyPrefix() }, { throwOnError: true }),
				queryClient.invalidateQueries({ queryKey: queryKeys.systemBackups.projectConfig() }, { throwOnError: true })
			]),
		() => 'projects'
	);

	async function startPolicyRun(type: ScheduleType, policyId: string): Promise<string | undefined> {
		if (type === 'system') return extractActivityId(await systemBackupService.create({ policyId, recoveryKey: '' }));
		if (type === 'project') return (await systemBackupService.runProjectBackups(policyId)).activityId;
		return (await systemBackupService.runSystemVolumeBackups({ policyId })).activityId;
	}

	async function runPolicy(type: ScheduleType, policyId: string) {
		runningPolicyId = policyId;
		try {
			const started = await tryCatch(startPolicyRun(type, policyId));
			if (started.error !== null) {
				let failed = m.system_volume_backups_run_failed();
				if (type === 'system') failed = m.system_backups_create_failed();
				if (type === 'project') failed = m.failed_to_run_project_backups();
				toast.error(failed, { description: extractApiErrorMessage(started.error) });
				return;
			}
			(type === 'project' ? projectActivity : backupActivity).accepted(started.data);
			toast.success(m.backups_started(), activityToastOptions(started.data, false));
		} finally {
			runningPolicyId = null;
		}
	}

	async function createBackup() {
		const customS3DestinationId = destination === 'local' ? '' : s3DestinationId;
		if (backupType === 'system') {
			let request: CreateSystemBackupRequest = { policyId: backupConfiguration, recoveryKey };
			if (backupConfiguration === 'custom') {
				request = { destination, s3DestinationId: customS3DestinationId, recoveryKey };
			}
			const result = await systemBackupService.create(request);
			backupActivity.accepted(extractActivityId(result));
			toast.success(m.backups_started(), activityToastOptions(extractActivityId(result), false));
		} else {
			let request: RunSystemVolumeBackupsRequest = { policyId: backupConfiguration };
			if (backupConfiguration === 'custom') {
				request = {
					custom: {
						destination,
						s3DestinationId: customS3DestinationId,
						stopContainers,
						selectionMode,
						volumeNames,
						ignoreAnonymous
					}
				};
			}
			const result = await systemBackupService.runSystemVolumeBackups(request);
			backupActivity.accepted(result.activityId);
			toast.success(m.backups_started(), activityToastOptions(result.activityId, false));
		}
		actionOpen = false;
		loading = false;
		void tryCatch(
			queryClient.invalidateQueries({ queryKey: queryKeys.systemBackups.historyPrefix() }, { throwOnError: true })
		).then((result) => {
			if (result.error === null) return;
			toast.error(m.common_refresh_failed({ resource: m.backup_history() }), {
				description: extractApiErrorMessage(result.error)
			});
		});
	}

	async function performAction() {
		if (action === 'create') {
			await createBackup();
		} else if (action === 'restore' && selected) {
			await systemBackupService.restore(selected.environmentId, selected.id, recoveryKey);
			toast.success(m.system_backups_restore_started());
		} else if (action === 'upload' && selected) {
			await systemBackupService.upload(selected.environmentId, selected.id, s3DestinationId, recoveryKey);
			toast.success(m.backups_upload_s3_success());
			await refresh();
		} else if (action === 'delete' && selected?.resourceType === 'project') {
			await projectService.deleteBackup(selected.resourceId ?? '', selected.id, selected.environmentId);
			toast.success(m.common_delete_success({ resource: m.project_backup() }));
			await refresh();
		} else if (action === 'delete' && selected) {
			await systemBackupService.delete(selected.environmentId, selected.id, recoveryKey);
			toast.success(m.system_backups_deleted());
			await refresh();
		} else if (action === 'discover') {
			const count = await systemBackupService.discover(s3DestinationId, recoveryKey);
			toast.success(m.system_backups_discovered({ count }));
			await refresh();
		}
		actionOpen = false;
	}

	async function submitAction() {
		if (loading || invalid || (action === 'create' && backupActivity.activeIds.length)) return;
		loading = true;
		try {
			const performed = await tryCatch(performAction());
			if (performed.error !== null) {
				toast.error(performed.error.message || actionFailedMessage());
			}
		} finally {
			loading = false;
		}
	}

	const actionButtons: ActionButton[] = $derived.by(() => {
		const buttons: ActionButton[] = [];
		if (canCreateBackups || canUpdateBackupPolicies) {
			const menuItems: ActionButtonMenuItem[] = [];
			if (canUpdateBackupPolicies) {
				menuItems.push({ id: 'schedule', label: m.jobs_schedule(), onclick: () => openSchedule() });
			}
			if (canCreateBackups) {
				menuItems.push({
					id: 'workspace-backup',
					label: m.volumes_workspace_backup(),
					onclick: () => openAction('create'),
					disabled: backupActivity.activeIds.length > 0
				});
			}
			buttons.push({
				id: 'create',
				action: 'create',
				placement: 'primary',
				label: m.common_create(),
				disabled: isReadOnly,
				menuItems
			});
		}
		if (hasAnyPermission(['s3-destinations:list', 's3-destinations:read'], GLOBAL_SCOPE)) {
			buttons.push({
				id: 's3-destinations',
				action: 'edit',
				icon: CloudStorageIcon,
				label: m.s3_destinations_title(),
				href: '/system/backups/s3'
			});
		}
		if (canDiscoverBackups && !policyCollection.recoveryKeyStored) {
			buttons.push({
				id: 'discover',
				action: 'inspect',
				label: m.system_backups_discover(),
				onclick: () => openAction('discover'),
				disabled: isReadOnly || data.destinations.length === 0
			});
		}
		if (canManageRecoveryKey) {
			let recoveryKeyLabel = m.system_backups_create_recovery_key();
			if (policyCollection.recoveryKeyStored) {
				recoveryKeyLabel = m.system_backups_reset_recovery_key();
			}
			buttons.push({
				id: 'recovery-key',
				action: 'edit',
				icon: LockIcon,
				label: m.system_backups_recovery_key(),
				disabled: isReadOnly,
				menuItems: [
					{
						id: 'recovery-key-set',
						label: recoveryKeyLabel,
						icon: ResetIcon,
						onclick: () => openRecoveryKey()
					},
					{
						id: 'recovery-key-import',
						label: m.system_backups_import_recovery_key(),
						icon: UploadIcon,
						onclick: () => openImportKey()
					}
				]
			});
		}
		return buttons;
	});
</script>

{#snippet addScheduleAction(type: ScheduleType)}
	{#if canUpdateBackupPolicies}
		<ArcaneButton
			action="create"
			size="sm"
			customLabel={m.volume_backup_add_schedule()}
			icon={AddIcon}
			onclick={() => openSchedule(type)}
			disabled={isReadOnly}
			data-testid={`backup-add-schedule-${type}`}
		/>
	{/if}
{/snippet}

{#snippet backupsEmptyState()}
	<EmptyState
		icon={BackupIcon}
		title={m.system_backups_empty_title()}
		description={m.system_backups_empty_description()}
		actionLabel={isReadOnly || !canCreateBackups ? undefined : m.common_create()}
		onAction={() => openAction('create')}
	/>
{/snippet}

{#snippet recoveryKeySummary()}
	{#if !policyCollection.recoveryKeyStored}
		<div class="flex items-center justify-between gap-3 rounded-md border border-warning/40 bg-warning/5 px-3 py-2">
			<div class="flex min-w-0 items-center gap-2">
				<LockIcon class="size-4 shrink-0 text-warning" />
				<p class="text-sm">{m.system_backups_recovery_key_needed()}</p>
			</div>
			{#if canManageRecoveryKey}
				<ArcaneButton
					action="edit"
					customLabel={m.system_backups_setup_recovery_key()}
					loading={generatingKey}
					onclick={openRecoveryKey}
					disabled={isReadOnly}
				/>
			{/if}
		</div>
	{/if}
{/snippet}

{#snippet recoveryKeyDialog()}
	<ResponsiveDialog
		bind:open={keyOpen}
		title={m.system_backups_recovery_key()}
		description={m.system_backups_recovery_key_saved()}
		contentClass={action === 'create' ? 'sm:max-w-190' : 'sm:max-w-140'}
	>
		<div class="space-y-3 py-2">
			<div class="flex items-center gap-2">
				<Input id="system-backup-recovery-key" value={newRecoveryKey} readonly spellcheck={false} mono />
				<CopyButton text={newRecoveryKey} variant="outline" tabindex={0} class="shrink-0" />
			</div>
			<Alert.Root variant="destructive">
				<AlertIcon class="size-4" />
				<Alert.Description>{m.system_backups_recovery_key_alert()}</Alert.Description>
			</Alert.Root>
		</div>
		{#snippet footer()}
			<ArcaneButton action="confirm" customLabel={m.common_done()} onclick={() => (keyOpen = false)} />
		{/snippet}
	</ResponsiveDialog>
{/snippet}

{#snippet importKeyDialog()}
	<ResponsiveDialog
		bind:open={importKeyOpen}
		title={m.system_backups_import_recovery_key()}
		description={m.system_backups_import_recovery_key_description()}
		contentClass="sm:max-w-140"
	>
		<div class="space-y-3 py-2">
			<TextInputWithLabel
				value={importKeyInput}
				onChange={(value) => (importKeyInput = value)}
				label={m.system_backups_recovery_key()}
				description={m.system_backups_recovery_key_required()}
				type="password"
				autocomplete="current-password"
			/>
			{#if policyCollection.recoveryKeyStored}
				<Alert.Root variant="destructive">
					<AlertIcon class="size-4" />
					<Alert.Description>{m.system_backups_import_recovery_key_alert()}</Alert.Description>
				</Alert.Root>
			{/if}
		</div>
		{#snippet footer()}
			<ArcaneButton action="cancel" onclick={() => (importKeyOpen = false)} disabled={importingKey} />
			<ArcaneButton
				action="save"
				customLabel={m.system_backups_import_recovery_key()}
				onclick={importRecoveryKey}
				loading={importingKey}
				disabled={importingKey || importKeyInvalid}
			/>
		{/snippet}
	</ResponsiveDialog>
{/snippet}

{#snippet actionDialog()}
	<ResponsiveDialog
		bind:open={actionOpen}
		title={dialogTitle()}
		description={dialogDescription()}
		contentClass={action === 'create' ? 'sm:max-w-190' : 'sm:max-w-140'}
	>
		<div class="space-y-5 py-2">
			{#if action === 'restore'}
				<Alert.Root size="sm">
					<InfoIcon class="size-4" />
					<Alert.Description>{m.restore_leaves_projects_description()}</Alert.Description>
				</Alert.Root>
			{/if}
			{#if action === 'create'}
				<SelectWithLabel
					id="manual-backup-type"
					value={backupType}
					onValueChange={changeBackupType}
					label={m.backups_backup_type()}
					description={m.backups_backup_type_description()}
					options={backupTypeOptions}
				/>
				<SelectWithLabel
					id="manual-backup-configuration"
					value={backupConfiguration}
					onValueChange={(value) => (backupConfiguration = value)}
					label={m.system_backups_backup_configuration()}
					options={configurationOptions}
				/>
				{#if backupConfiguration === 'custom'}
					<SelectWithLabel
						id="manual-backup-destination"
						value={destination}
						onValueChange={(value) => (destination = value as SystemBackupDestination)}
						label={m.backups_destination_label()}
						options={destinationOptions}
					/>
					{#if backupType === 'volume'}
						<LabeledSwitch
							id="manual-volume-backup-stop-containers"
							checked={stopContainers}
							onCheckedChange={(value) => (stopContainers = value)}
							label={m.volume_backup_stop_containers()}
							description={m.volume_backup_stop_containers_description()}
						/>
					{/if}
				{/if}
			{/if}
			{#if action === 'upload' || action === 'discover' || (action === 'create' && backupConfiguration === 'custom' && destination !== 'local')}
				<SelectWithLabel
					id="manual-system-backup-s3"
					value={s3DestinationId}
					onValueChange={(value) => (s3DestinationId = value)}
					label={m.volume_backup_s3_destination_label()}
					error={destinationError || null}
					options={s3Options}
				/>
			{/if}
			{#if action === 'create' && backupType === 'volume' && backupConfiguration === 'custom'}
				<BackupScopeFields
					idPrefix="manual-volume-backup"
					kind="volume"
					{selectionMode}
					selected={volumeNames}
					{ignoreAnonymous}
					options={systemVolumeOptions}
					loading={systemVolumeOptionsLoading}
					onChange={(values) => {
						selectionMode = values.selectionMode ?? selectionMode;
						volumeNames = values.selected ?? volumeNames;
						ignoreAnonymous = values.ignoreAnonymous ?? ignoreAnonymous;
					}}
				/>
			{/if}
			{#if actionNeedsTypedKey}
				<TextInputWithLabel
					value={recoveryKey}
					onChange={(value) => (recoveryKey = value)}
					label={m.system_backups_recovery_key()}
					description={policyCollection.recoveryKeyStored
						? m.system_backups_recovery_key_saved_description()
						: m.system_backups_recovery_key_enter_description()}
					type="password"
					autocomplete="current-password"
				/>
			{:else if action !== 'delete' && !selectedRemote && !(action === 'create' && backupType === 'volume') && !policyCollection.recoveryKeyStored}
				<div class="flex items-center justify-between gap-3 rounded-md border px-3 py-2">
					<p class="text-sm text-muted-foreground">{m.system_backups_recovery_key_needed()}</p>
					<ArcaneButton
						action="edit"
						customLabel={m.system_backups_setup_recovery_key()}
						loading={generatingKey}
						onclick={() => {
							actionOpen = false;
							openRecoveryKey();
						}}
					/>
				</div>
			{/if}
		</div>
		{#snippet footer()}
			<ArcaneButton action="cancel" onclick={() => (actionOpen = false)} disabled={loading} />
			<ArcaneButton
				action={submitButtonAction[action]}
				customLabel={dialogTitle()}
				onclick={submitAction}
				{loading}
				disabled={loading || invalid || (action === 'create' && backupActivity.activeIds.length > 0)}
			/>
		{/snippet}
	</ResponsiveDialog>
{/snippet}

{#snippet restoreFilesDialog()}
	<ResponsiveDialog
		bind:open={restoreFilesOpen}
		onOpenChange={(open) => {
			if (!open) closeRestoreFiles();
		}}
		title={m.volume_restore_files()}
		description={m.system_backups_restore_files_description()}
		contentClass="sm:max-w-160"
	>
		<div class="space-y-3 py-2">
			<Alert.Root size="sm">
				<InfoIcon class="size-4" />
				<Alert.Description>
					{m.system_backups_restore_files_lifecycle_info()}
					{m.system_backups_restore_files_current_directory()}
				</Alert.Description>
			</Alert.Root>

			{#if !restoreFilesRemote}
				<div class="flex items-end gap-2">
					<div class="min-w-0 flex-1">
						<TextInputWithLabel
							value={restoreFilesRecoveryKey}
							onChange={updateRestoreFilesRecoveryKey}
							label={m.system_backups_recovery_key()}
							description={policyCollection.recoveryKeyStored
								? m.system_backups_recovery_key_saved_description()
								: m.system_backups_recovery_key_enter_description()}
							type="password"
							autocomplete="current-password"
						/>
					</div>
					<ArcaneButton
						action="inspect"
						customLabel={m.system_backups_load_files()}
						onclick={loadRestoreFiles}
						disabled={restoreFilesKeyInvalid}
					/>
				</div>
			{/if}

			{#if restoreFilesProvider}
				{#key restoreFilesProvider}
					<BackupFilePicker
						provider={restoreFilesProvider}
						bind:selectedPaths={restoreFilesSelectedPaths}
						bind:selectAll={restoreFilesSelectAll}
						bind:search={restoreFilesSearch}
						onRootLoad={updateRestoreFilesRootLoad}
					/>
				{/key}

				<Alert.Root variant="warning" size="sm">
					<AlertIcon class="size-4" />
					<Alert.Description>
						{m.volumes_backup_overwrite_warning()}
					</Alert.Description>
				</Alert.Root>
			{/if}
		</div>

		{#snippet footer()}
			<ArcaneButton action="cancel" onclick={closeRestoreFiles} disabled={restoringFiles} />
			<ArcaneButton
				action="confirm"
				customLabel={m.volume_restore_files()}
				onclick={restoreSelectedFiles}
				loading={restoringFiles}
				disabled={restoringFiles || !restoreFilesLoaded || (!restoreFilesSelectAll && restoreFilesSelectedPaths.length === 0)}
			/>
		{/snippet}
	</ResponsiveDialog>
{/snippet}

<SettingsPageLayout
	title={m.system_backups_title()}
	description={m.system_backups_description()}
	pageType="management"
	showReadOnlyTag={isReadOnly}
	{actionButtons}
>
	{#snippet mainContent()}
		<div class="space-y-8">
			{#if localDockerDisabled}
				<Alert.Root variant="warning-subtle">
					<InfoIcon class="size-4 text-warning" />
					<Alert.Description>{m.local_docker_disabled_backups_unavailable()}</Alert.Description>
				</Alert.Root>
			{/if}
			{@render recoveryKeySummary()}

			<SettingsSection title={m.system()} description={m.arcane_only_backup_description()} variant="plain">
				{#snippet actions()}{@render addScheduleAction('system')}{/snippet}
				{#if policyCollection.policies.length}
					<div class="grid grid-cols-1 gap-1.5 text-xs text-muted-foreground lg:grid-cols-2">
						{#each policyCollection.policies as policy (policy.id)}
							<BackupPolicyCard
								{policy}
								resourceType="system"
								onEdit={() => openSchedule('system', policy.id)}
								editDisabled={isReadOnly || !canUpdateBackupPolicies}
								onRun={() => runPolicy('system', policy.id)}
								runDisabled={isReadOnly ||
									!canCreateBackups ||
									!policyCollection.recoveryKeyStored ||
									backupActivity.activeIds.length > 0 ||
									runningPolicyId !== null}
								running={runningPolicyId === policy.id}
							/>
						{/each}
					</div>
				{:else}
					<p class="text-sm text-muted-foreground">{m.system_backups_no_schedules()}</p>
				{/if}
			</SettingsSection>

			<SettingsSection title={m.projects_title()} description={m.back_up_selected_projects()} variant="plain">
				{#snippet actions()}{@render addScheduleAction('project')}{/snippet}
				{#if projectPolicyCollection.policies.length}
					<div class="grid grid-cols-1 gap-1.5 text-xs text-muted-foreground lg:grid-cols-2">
						{#each projectPolicyCollection.policies as policy (policy.id)}
							<BackupPolicyCard
								{policy}
								resourceType="project"
								showStopContainers
								onEdit={() => openSchedule('project', policy.id)}
								editDisabled={isReadOnly || !canUpdateBackupPolicies}
								onRun={() => runPolicy('project', policy.id)}
								runDisabled={isReadOnly ||
									!canCreateBackups ||
									!policy.enabled ||
									projectActivity.activeIds.length > 0 ||
									runningPolicyId !== null}
								running={runningPolicyId === policy.id}
							/>
						{/each}
					</div>
				{:else}
					<p class="text-sm text-muted-foreground">{m.system_backups_no_schedules()}</p>
				{/if}
			</SettingsSection>

			<SettingsSection title={m.resource_volumes_cap()} description={m.system_backups_type_volume_description()} variant="plain">
				{#snippet actions()}{@render addScheduleAction('volume')}{/snippet}
				{#if systemVolumePolicyCollection.policies.length}
					<div class="grid grid-cols-1 gap-1.5 text-xs text-muted-foreground lg:grid-cols-2">
						{#each systemVolumePolicyCollection.policies as policy (policy.id)}
							<BackupPolicyCard
								{policy}
								resourceType="volume"
								showStopContainers
								onEdit={() => openSchedule('volume', policy.id)}
								editDisabled={isReadOnly || !canUpdateBackupPolicies}
								onRun={() => runPolicy('volume', policy.id)}
								runDisabled={isReadOnly || !canCreateBackups || backupActivity.activeIds.length > 0 || runningPolicyId !== null}
								running={runningPolicyId === policy.id}
							/>
						{/each}
					</div>
				{:else}
					<p class="text-sm text-muted-foreground">{m.system_backups_no_schedules()}</p>
				{/if}
			</SettingsSection>

			{#if unmanagedBackupRetentionDays !== undefined}
				<p class="text-sm text-muted-foreground">
					{Number(unmanagedBackupRetentionDays) > 0
						? m.unmanaged_backup_retention_summary({ days: Number(unmanagedBackupRetentionDays) })
						: m.unmanaged_backup_retention_disabled_summary()}
					<a href={resolve('/(app)/settings/storage')} class="ml-1 text-primary-text hover:underline">{m.common_edit()}</a>
				</p>
			{/if}

			{#each [...backupActivity.activeIds, ...projectActivity.activeIds] as activityId (activityId)}
				<Alert.Root
					><Alert.Description>
						{m.backups_running()}
						<ArcaneButton
							action="inspect"
							customLabel={m.activity_view_activity()}
							onclick={() => activityStore.openCenter(activityId)}
						/>
					</Alert.Description></Alert.Root
				>
			{/each}

			<SystemBackupTable
				emptyState={backupsEmptyState}
				{backups}
				{localDockerDisabled}
				bind:requestOptions={pageState.requestOptions}
				onRetry={() => backupsQuery.refetch()}
				onRestore={(item) => openAction('restore', item)}
				onRestoreFiles={openRestoreFiles}
				onRestoreVolume={openVolumeRestore}
				onOpenProject={openProjectBackups}
				onRestoreProjectAsNew={openRestoreAsNew}
				onUpload={(item) => openAction('upload', item)}
				onDelete={(item) => openAction('delete', item)}
				onOpenVolume={openVolumeBackups}
			/>
		</div>
	{/snippet}
	{#snippet additionalContent()}
		{#if scheduleSession > 0}
			{#key scheduleSession}
				<SystemBackupScheduleDialog
					bind:open={scheduleOpen}
					initialType={scheduleType}
					policyId={editingScheduleId}
					systemPolicies={policyCollection.policies}
					projectPolicies={projectPolicyCollection.policies}
					volumePolicies={systemVolumePolicyCollection.policies}
					recoveryKeyStored={policyCollection.recoveryKeyStored}
					destinations={data.destinations}
					volumeOptions={systemVolumeOptions}
					volumeOptionsLoading={systemVolumeOptionsLoading}
					onLoadVolumeOptions={loadSystemVolumeOptions}
					onSystemSaved={(policies) => {
						policyCollection = { ...policyCollection, policies };
						void queryClient.invalidateQueries({ queryKey: queryKeys.systemBackups.policies() });
					}}
					onProjectSaved={(policies) => queryClient.setQueryData(projectBackupConfigQuery().queryKey, { policies })}
					onVolumeSaved={(policies) => {
						systemVolumePolicyCollection = { policies };
						void queryClient.invalidateQueries({ queryKey: queryKeys.systemBackups.systemVolumeConfig() });
					}}
				/>
			{/key}
		{/if}

		{@render recoveryKeyDialog()}
		{@render importKeyDialog()}
		{@render actionDialog()}
		{@render restoreFilesDialog()}
		<ProjectRestoreAsNewDialog
			bind:open={restoreAsNewOpen}
			backup={restoreAsNewBackup}
			environmentId={restoreAsNewEnvironmentId}
		/>
	{/snippet}
</SettingsPageLayout>
