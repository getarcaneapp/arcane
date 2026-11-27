<script lang="ts">
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import { backupRunColumns, backupRunMobileFields } from '#lib/components/arcane-table/backup-columns.js';
	import BackupDestinationCell from '#lib/components/arcane-table/cells/backup-destination-cell.svelte';
	import BackupSizeCell from '#lib/components/arcane-table/cells/backup-size-cell.svelte';
	import BackupStatusCell from '#lib/components/arcane-table/cells/backup-status-cell.svelte';
	import BackupTriggerCell from '#lib/components/arcane-table/cells/backup-trigger-cell.svelte';
	import CreatedAtCell from '#lib/components/arcane-table/cells/created-at-cell.svelte';
	import { UniversalMobileCard, type ColumnSpec, type MobileFieldVisibility } from '#lib/components/arcane-table/index.js';
	import BackupPolicyCard from '#lib/components/backup-policy-card.svelte';
	import BackupPolicyDialog from '#lib/components/backup-policy-dialog.svelte';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import ProjectRestoreAsNewDialog from '#lib/components/dialogs/project-restore-as-new-dialog.svelte';
	import RowActionsMenu from '#lib/components/file-browser/row-actions-menu.svelte';
	import LabeledSwitch from '#lib/components/form/labeled-switch.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import ProjectVolumeChecklist from '#lib/components/project-volume-checklist.svelte';
	import { EmptyState, LoadingState } from '#lib/components/states/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { useBackupActivity } from '#lib/hooks/use-backup-activity.svelte.js';
	import {
		AddIcon,
		AlertIcon,
		BackupIcon,
		ClockIcon,
		CopyIcon,
		DownloadIcon,
		InfoIcon,
		RestartIcon,
		TrashIcon,
		VolumesIcon
	} from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { s3DestinationAllQuery } from '#lib/query/backups.js';
	import { projectBackupListQuery, projectBackupOptionsQuery, projectBackupPoliciesQuery } from '#lib/query/projects.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { activityService } from '#lib/services/activity-service.js';
	import { projectService } from '#lib/services/project-service.js';
	import { activityStore } from '#lib/stores/activity.store.svelte.js';
	import type { S3Destination, SystemBackupDestination } from '#lib/types/backup.js';
	import type { Paginated } from '#lib/types/base.js';
	import type {
		ProjectBackup,
		ProjectBackupPolicy,
		ProjectExportOptions,
		ProjectVolume,
		UpdateProjectBackupPolicy
	} from '#lib/types/project.js';
	import { GLOBAL_SCOPE } from '#lib/types/role.js';
	import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
	import { activityToastOptions } from '#lib/utils/activity-toast.js';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import {
		backupDestinationDisplay,
		backupDestinationOptions,
		backupPolicyUpdateFromPolicy,
		backupStatusLabel,
		backupTriggerLabel,
		restorableVolumes,
		s3DestinationOptions
	} from '#lib/utils/backups.js';
	import { bytes, formatDateTimeShort } from '#lib/utils/formatting.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let {
		projectId,
		environmentId,
		running,
		onRestored
	}: {
		projectId: string;
		environmentId: string;
		// Whether the project runs decides if the restore stops it and offers to deploy it again.
		running: boolean;
		// Called when a restore ends, so the page reloads what it shows; files says the project files were restored.
		onRestored?: (restored: { files: boolean; succeeded: boolean }) => Promise<unknown>;
	} = $props();

	const canBackup = $derived(hasPermission('projects:backup', environmentId));
	const canRestore = $derived(hasPermission('projects:restore', environmentId));
	const canRestoreAsNew = $derived(hasPermission('projects:create', environmentId));
	const canRestoreFiles = $derived(hasPermission('projects:update', environmentId));
	const canTrackActivities = $derived(hasPermission('activities:list', environmentId));
	const canReadActivities = $derived(hasPermission('activities:read', environmentId));
	// Destinations are a global resource; an environment-scoped grant does not cover them.
	const canListS3Destinations = $derived(hasPermission('s3-destinations:list', GLOBAL_SCOPE));

	let requestOptions = $state<SearchPaginationSortRequest>({ pagination: { page: 1, limit: 10 } });
	let mobileFieldVisibility = $state<Record<string, boolean>>({});

	let dialogOpen = $state(false);
	let options = $state<ProjectExportOptions | null>(null);
	let optionsError = $state<string | null>(null);
	let checkedOverrides = $state<Record<string, boolean>>({});
	let stopContainers = $state(true);
	let destination = $state<SystemBackupDestination>('local');
	let s3DestinationId = $state('');
	let creating = $state(false);

	let restoreOpen = $state(false);
	let restoreAsNewOpen = $state(false);
	let restoreItem = $state<ProjectBackup | null>(null);
	let restoreFiles = $state(true);
	let restoreOverrides = $state<Record<string, boolean>>({});
	let redeploy = $state(true);
	let restoring = $state(false);

	let scheduleOpen = $state(false);
	let scheduleSession = $state(0);
	let editingPolicyId = $state<string | undefined>();
	// The edited policy's volumes; undefined follows the volumes a manual backup includes by default.
	let scheduleBaseVolumes = $state<string[] | undefined>();
	let scheduleOverrides = $state<Record<string, boolean>>({});
	const scheduleOptionsQuery = createQuery(() => ({
		...projectBackupOptionsQuery(environmentId, projectId),
		enabled: scheduleOpen
	}));
	const scheduleFollowsProject = $derived(scheduleBaseVolumes === undefined && Object.keys(scheduleOverrides).length === 0);
	const policiesQuery = createQuery(() => projectBackupPoliciesQuery(environmentId, projectId));
	const policies = $derived<ProjectBackupPolicy[]>(policiesQuery.data?.policies ?? []);
	// Saving sends the whole list, so schedules are only edited once the current list has loaded.
	const policiesLoaded = $derived(policiesQuery.isSuccess);
	const backupSelectable = (volume: ProjectVolume) => volume.included;
	const backupVolumeNotes = {
		backupStore: m.backup_store_not_backed_up(),
		missing: m.not_created_yet_nothing_to_back_up(),
		driverOpts: m.driver_options_not_backed_up(),
		external: m.external_volume_not_backed_up()
	};
	const selectedVolumes = $derived(
		(options?.volumes ?? []).filter((volume) => backupSelectable(volume) && (checkedOverrides[volume.key] ?? volume.included))
	);
	const needsS3Destination = $derived(destination !== 'local');
	const restoreVolumes = $derived(restorableVolumes(restoreItem?.volumes ?? []));
	const selectedRestoreVolumes = $derived(restoreVolumes.filter((volume) => restoreOverrides[volume.key] ?? true));

	const queryClient = useQueryClient();
	const backupsQuery = createQuery(() => ({
		...projectBackupListQuery(environmentId, projectId, requestOptions),
		placeholderData: (previous, query) =>
			query?.queryKey[1] === environmentId && query?.queryKey[2] === projectId ? previous : undefined
	}));
	const backups = $derived<Paginated<ProjectBackup>>(
		backupsQuery.data ?? { data: [], pagination: { currentPage: 1, totalPages: 1, totalItems: 0, itemsPerPage: 10 } }
	);

	function invalidateBackups() {
		return Promise.all([
			queryClient.invalidateQueries({ queryKey: queryKeys.projects.backupListPrefix(environmentId, projectId) }),
			queryClient.invalidateQueries({ queryKey: queryKeys.projects.backupPolicies(environmentId, projectId) })
		]);
	}

	const backupActivity = useBackupActivity(
		() => environmentId,
		(activity) =>
			(activity.metadata?.['action'] === 'create_project_backup' ||
				activity.metadata?.['action'] === 'scheduled_project_backup') &&
			activity.resourceId === projectId,
		invalidateBackups,
		() => `project:${projectId}`,
		() => {
			if (canTrackActivities) return undefined;
			const id = projectId;
			return async (environmentId) => {
				const result = await projectService.listBackups(id, { pagination: { page: 1, limit: 100 } }, environmentId);
				return result.data.filter((backup) => backup.status === 'running').map((backup) => backup.id);
			};
		}
	);

	// A global run backs up many projects under one activity, so it only refreshes this list.
	useBackupActivity(
		() => environmentId,
		(activity) => ['run_project_backups', 'scheduled_project_backups'].includes(String(activity.metadata?.['action'])),
		invalidateBackups,
		() => 'global-project-backups'
	);

	const restoreActivity = useBackupActivity(
		() => environmentId,
		(activity) => activity.metadata?.['action'] === 'restore_project_backup' && activity.resourceId === projectId,
		invalidateBackups,
		() => `project-restore:${projectId}`
	);

	// Plain variables, so the effect only reads state: it notices a restore that was running and no longer is. Only a
	// restore started here is known to have restored the project files.
	let restoreWasRunning = false;
	let restoringFiles = false;
	let restoreActivityId: string | undefined;
	$effect(() => {
		const restoring = restoreActivity.activeIds.length > 0;
		if (restoreWasRunning && !restoring) {
			void finishRestore(restoreActivityId, restoringFiles);
			restoringFiles = false;
		}
		if (restoring) restoreActivityId = restoreActivity.activeIds[0];
		restoreWasRunning = restoring;
	});

	async function finishRestore(activityId: string | undefined, files: boolean) {
		const result =
			activityId && canReadActivities ? await tryCatch(activityService.getActivity(activityId, environmentId, 1)) : undefined;
		await onRestored?.({ files, succeeded: result?.data?.activity.status === 'success' });
	}

	const s3DestinationsQuery = createQuery(() => ({
		...s3DestinationAllQuery(),
		enabled: canBackup && canListS3Destinations,
		retry: false,
		refetchOnWindowFocus: false
	}));
	const s3Destinations = $derived<S3Destination[]>(canBackup && canListS3Destinations ? (s3DestinationsQuery.data ?? []) : []);
	const s3DestinationsError = $derived(s3DestinationsQuery.error ? extractApiErrorMessage(s3DestinationsQuery.error) : null);

	async function openDialog() {
		dialogOpen = true;
		options = null;
		optionsError = null;
		checkedOverrides = {};
		stopContainers = true;
		destination = 'local';
		s3DestinationId = s3Destinations.length === 1 ? (s3Destinations[0]?.id ?? '') : '';
		const result = await tryCatch(projectService.getBackupOptions(projectId, environmentId));
		if (result.error !== null) {
			optionsError = extractApiErrorMessage(result.error);
			return;
		}
		options = result.data;
	}

	async function handleCreate() {
		if (!options || creating) return;
		creating = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(
				projectService.createBackup(
					projectId,
					{
						volumes: selectedVolumes.map((volume) => volume.key),
						stopContainers,
						destination,
						s3DestinationId: needsS3Destination ? s3DestinationId : undefined
					},
					environmentId
				)
			),
			message: m.failed_to_start_project_backup(),
			setLoadingState: (value) => (creating = value),
			onSuccess: (data) => {
				dialogOpen = false;
				backupActivity.accepted(data.activityId);
				toast.success(m.backups_started(), canReadActivities ? activityToastOptions(data.activityId, false) : undefined);
				void invalidateBackups();
			}
		});
	}

	function handleDelete(backup: ProjectBackup) {
		openConfirmDialog({
			title: m.common_remove_title({ resource: m.project_backup() }),
			message: m.remove_this_backup(),
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
					await handleApiResultWithCallbacks({
						result: await tryCatch(projectService.deleteBackup(projectId, backup.id, environmentId)),
						message: m.common_remove_failed({ resource: m.project_backup() }),
						onSuccess: () => {
							toast.success(m.common_delete_success({ resource: m.project_backup() }));
						}
					});
					await invalidateBackups();
				}
			}
		});
	}

	function resetRestore(backup: ProjectBackup) {
		restoreItem = backup;
		restoreFiles = backup.files && canRestoreFiles;
		restoreOverrides = {};
		redeploy = true;
	}

	function openRestore(backup: ProjectBackup) {
		resetRestore(backup);
		restoreOpen = true;
	}

	function openRestoreAsNew(backup: ProjectBackup) {
		restoreItem = backup;
		restoreAsNewOpen = true;
	}

	async function handleRestore() {
		if (!restoreItem || restoring) return;
		restoring = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(
				projectService.restoreBackup(
					projectId,
					restoreItem.id,
					{
						files: restoreFiles,
						volumes: selectedRestoreVolumes.map((volume) => volume.key),
						redeploy: running ? redeploy : undefined
					},
					environmentId
				)
			),
			message: m.failed_to_restore_backup(),
			setLoadingState: (value) => (restoring = value),
			onSuccess: (data) => {
				restoreOpen = false;
				restoringFiles = restoreFiles;
				// Only activities say when a restore ends; without them the server still refuses a second one.
				restoreActivity.accepted(canTrackActivities ? data.activityId : undefined);
				toast.success(m.restore_started(), canReadActivities ? activityToastOptions(data.activityId, false) : undefined);
			}
		});
	}

	function openSchedule(policy?: ProjectBackupPolicy) {
		if (!policiesLoaded) return;
		editingPolicyId = policy?.id;
		scheduleBaseVolumes = policy?.volumes;
		scheduleOverrides = {};
		scheduleSession += 1;
		scheduleOpen = true;
	}

	function scheduleDefault(volume: ProjectVolume): boolean {
		return scheduleBaseVolumes ? scheduleBaseVolumes.includes(volume.key) : volume.included;
	}

	function scheduleChecked(volume: ProjectVolume): boolean {
		return backupSelectable(volume) && (scheduleOverrides[volume.key] ?? scheduleDefault(volume));
	}

	// An untouched selection keeps the policy's own, so one that follows the project keeps doing so.
	function scheduleVolumes(): string[] | undefined {
		if (Object.keys(scheduleOverrides).length === 0) return scheduleBaseVolumes;
		return (scheduleOptionsQuery.data?.volumes ?? []).filter(scheduleChecked).map((volume) => volume.key);
	}

	function includedVolumes(backup: ProjectBackup): string {
		const keys = backup.volumes.filter((volume) => volume.included).map((volume) => volume.key);
		return keys.length ? keys.join(', ') : m.files_only();
	}

	// Project backups are listed newest first only, so their columns don't sort.
	const columns = [
		{ accessorKey: 'createdAt', title: m.common_created(), cell: CreatedCell },
		...backupRunColumns<ProjectBackup>({
			status: StatusCell,
			trigger: TriggerCell,
			destination: DestinationCell,
			size: SizeCell
		}).map((column) => ({ ...column, sortable: false })),
		{ accessorKey: 'volumes', title: m.resource_volumes_cap(), cell: VolumesCell },
		{ accessorKey: 'error', title: m.common_error(), cell: ErrorCell }
	] satisfies ColumnSpec<ProjectBackup>[];

	// Project backups have no management type.
	const mobileFields = [
		...backupRunMobileFields().filter((field) => field.id !== 'type'),
		{ id: 'volumes', label: m.resource_volumes_cap(), defaultVisible: true }
	];
</script>

{#snippet CreatedCell({ item }: { item: ProjectBackup })}
	<CreatedAtCell value={item.createdAt} />
{/snippet}

{#snippet StatusCell({ item }: { item: ProjectBackup })}
	<BackupStatusCell status={item.status} />
{/snippet}

{#snippet TriggerCell({ item }: { item: ProjectBackup })}
	<BackupTriggerCell trigger={item.trigger} />
{/snippet}

{#snippet VolumesCell({ item }: { item: ProjectBackup })}
	<span class="line-clamp-2 max-w-64 text-xs break-all">{includedVolumes(item)}</span>
{/snippet}

{#snippet DestinationCell({ item }: { item: ProjectBackup })}
	<BackupDestinationCell {item} />
{/snippet}

{#snippet SizeCell({ item }: { item: ProjectBackup })}
	<BackupSizeCell size={item.size} />
{/snippet}

{#snippet ErrorCell({ item }: { item: ProjectBackup })}
	<span class="line-clamp-2 max-w-48 text-xs break-words text-destructive" title={item.error}>{item.error || '-'}</span>
{/snippet}

{#snippet RowActions({ item }: { item: ProjectBackup })}
	{#if canBackup || canRestore || canRestoreAsNew}
		<RowActionsMenu openMenuLabel={m.actions_for_name({ name: formatDateTimeShort(item.createdAt) })}>
			{#if canRestore}
				<DropdownMenu.Item
					disabled={item.status !== 'succeeded' || restoreActivity.activeIds.length > 0}
					onclick={() => openRestore(item)}
				>
					<RestartIcon class="size-4" />
					{m.volumes_backups_restore()}
				</DropdownMenu.Item>
			{/if}
			{#if canRestoreAsNew && item.files}
				<DropdownMenu.Item disabled={item.status !== 'succeeded'} onclick={() => openRestoreAsNew(item)}>
					<CopyIcon class="size-4" />
					{m.restore_as_new_project()}
				</DropdownMenu.Item>
			{/if}
			{#if canBackup}
				<DropdownMenu.Item variant="destructive" disabled={item.status === 'running'} onclick={() => handleDelete(item)}>
					<TrashIcon class="size-4" />
					{m.common_remove()}
				</DropdownMenu.Item>
			{/if}
		</RowActionsMenu>
	{/if}
{/snippet}

{#snippet BackupMobileCard({
	item,
	mobileFieldVisibility
}: {
	item: ProjectBackup;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={{ component: VolumesIcon, variant: 'blue' }}
		title={(item) => formatDateTimeShort(item.createdAt)}
		fields={[
			{
				label: m.common_status(),
				getValue: (item) => backupStatusLabel(item.status),
				icon: InfoIcon,
				iconVariant: 'gray',
				show: mobileFieldVisibility['status'] ?? true
			},
			{
				label: m.volume_backup_trigger(),
				getValue: (item) => backupTriggerLabel(item.trigger),
				icon: InfoIcon,
				iconVariant: 'gray',
				show: mobileFieldVisibility['trigger'] ?? true
			},
			{
				label: m.resource_volumes_cap(),
				getValue: includedVolumes,
				icon: VolumesIcon,
				iconVariant: 'gray',
				show: mobileFieldVisibility['volumes'] ?? true
			},
			{
				label: m.backups_destination_label(),
				getValue: backupDestinationDisplay,
				icon: DownloadIcon,
				iconVariant: 'gray',
				show: mobileFieldVisibility['destination'] ?? true
			},
			{
				label: m.common_size(),
				getValue: (item) => bytes.format(item.size, { unitSeparator: ' ' }) ?? '-',
				icon: InfoIcon,
				iconVariant: 'gray',
				show: mobileFieldVisibility['size'] ?? true
			},
			{
				label: m.common_error(),
				getValue: (item) => item.error,
				icon: AlertIcon,
				iconVariant: 'red',
				show: Boolean(item.error)
			}
		]}
		footer={{ label: m.common_created(), getValue: (item) => formatDateTimeShort(item.createdAt), icon: ClockIcon }}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet BackupsLoadingState()}
	<LoadingState icon={BackupIcon} title={m.loading_backups()} description={m.loading_backups_description()} />
{/snippet}

{#snippet BackupsEmptyState()}
	<EmptyState icon={BackupIcon} title={m.system_backups_empty_title()} description={m.project_backups_empty_description()} />
{/snippet}

<div class="min-w-0 space-y-4" data-testid="project-backups-panel">
	{#each restoreActivity.activeIds as activityId (activityId)}
		<Alert.Root>
			<Alert.Description>
				{m.restore_in_progress()}
				{#if canReadActivities}
					<ArcaneButton
						action="inspect"
						customLabel={m.activity_view_activity()}
						onclick={() => activityStore.openCenter(activityId, undefined, environmentId)}
					/>
				{/if}
			</Alert.Description>
		</Alert.Root>
	{/each}
	{#each backupActivity.activeIds as activityId (activityId)}
		<Alert.Root>
			<Alert.Description>
				{m.backups_running()}
				{#if canReadActivities}
					<ArcaneButton
						action="inspect"
						customLabel={m.activity_view_activity()}
						onclick={() => activityStore.openCenter(activityId, undefined, environmentId)}
					/>
				{/if}
			</Alert.Description>
		</Alert.Root>
	{/each}
	<div class="flex flex-col gap-3 sm:flex-row sm:items-start">
		<Alert.Root size="sm" class="min-w-0 flex-1">
			<InfoIcon class="size-4" />
			<Alert.Description>{m.one_snapshot_per_backup()}</Alert.Description>
		</Alert.Root>
		{#if canBackup}
			<ArcaneButton
				action="create"
				customLabel={m.back_up_now()}
				icon={AddIcon}
				loading={creating}
				disabled={creating || backupActivity.activeIds.length > 0}
				onclick={openDialog}
				class="shrink-0 self-end sm:self-auto"
				data-testid="project-backup-create"
			/>
		{/if}
	</div>
	<div class="min-w-0 space-y-2">
		<div class="flex flex-wrap items-center justify-between gap-2">
			<h3 class="text-sm font-medium">{m.system_backups_schedules()}</h3>
			{#if canBackup}
				<ArcaneButton
					action="create"
					size="sm"
					customLabel={m.volume_backup_add_schedule()}
					icon={AddIcon}
					disabled={!policiesLoaded}
					onclick={() => openSchedule()}
					data-testid="project-backup-add-schedule"
				/>
			{/if}
		</div>
		{#if policiesQuery.error}
			<Alert.Root variant="destructive" size="sm">
				<AlertIcon class="size-4" />
				<Alert.Description>{extractApiErrorMessage(policiesQuery.error)}</Alert.Description>
			</Alert.Root>
		{:else if policies.length === 0}
			<p class="text-sm text-muted-foreground">{m.no_backup_schedules()}</p>
		{:else}
			<div class="grid grid-cols-1 gap-1.5 text-xs text-muted-foreground sm:grid-cols-2">
				{#each policies as policy (policy.id)}
					<BackupPolicyCard {policy} showStopContainers onEdit={canBackup ? () => openSchedule(policy) : undefined} />
				{/each}
			</div>
		{/if}
	</div>
	<ArcaneTable
		persistKey="arcane-project-backup-table"
		items={backups}
		bind:requestOptions
		bind:mobileFieldVisibility
		selectionDisabled={true}
		withoutSearch={true}
		{columns}
		{mobileFields}
		rowActions={RowActions}
		mobileCard={BackupMobileCard}
		emptyState={BackupsEmptyState}
		loadingState={BackupsLoadingState}
		loading={backupsQuery.isPending}
		onRetry={() => backupsQuery.refetch()}
		refreshError={backupsQuery.error}
	/>
</div>

<ResponsiveDialog
	bind:open={dialogOpen}
	title={m.back_up_now()}
	description={m.back_up_files_and_volumes()}
	contentClass="sm:max-w-xl"
	dismissible={!creating}
>
	<div class="grid min-w-0 gap-y-4 py-4" data-testid="project-backup-dialog">
		{#if optionsError}
			<Alert.Root variant="destructive">
				<AlertIcon class="size-4" />
				<Alert.Description>{optionsError}</Alert.Description>
			</Alert.Root>
		{:else if !options}
			<div class="flex items-center gap-2 text-sm text-muted-foreground">
				<Spinner class="size-4" />
				<span>{m.common_loading()}</span>
			</div>
		{:else}
			{#if options.volumes.length === 0}
				<p class="text-sm text-muted-foreground">{m.no_volumes_files_only()}</p>
			{:else}
				<div class="min-w-0 space-y-2">
					<p class="text-sm font-medium">{m.include_volume_data()}</p>
					<ProjectVolumeChecklist
						volumes={options.volumes}
						bind:overrides={checkedOverrides}
						selectable={backupSelectable}
						notes={backupVolumeNotes}
						idPrefix="project-backup-volume"
						disabled={creating}
					/>
				</div>
			{/if}
			<LabeledSwitch
				id="project-backup-stop-containers"
				checked={stopContainers}
				label={m.volume_backup_stop_containers()}
				description={m.stop_containers_during_snapshot()}
				disabled={creating}
				onCheckedChange={(next) => (stopContainers = next)}
			/>
			{#if s3DestinationsError}
				<Alert.Root variant="destructive" size="sm">
					<AlertIcon class="size-4" />
					<Alert.Description>{m.s3_destinations_load_failed()}: {s3DestinationsError}</Alert.Description>
				</Alert.Root>
			{/if}
			<SelectWithLabel
				id="project-backup-destination"
				value={destination}
				onValueChange={(value) => (destination = value as SystemBackupDestination)}
				label={m.backups_destination_label()}
				options={backupDestinationOptions(s3Destinations.length > 0)}
				disabled={creating}
			/>
			{#if needsS3Destination}
				<SelectWithLabel
					id="project-backup-s3-destination"
					value={s3DestinationId}
					onValueChange={(value) => (s3DestinationId = value)}
					label={m.volume_backup_s3_destination_label()}
					options={s3DestinationOptions(s3Destinations)}
					disabled={creating}
				/>
			{/if}
		{/if}
	</div>

	{#snippet footer()}
		<ArcaneButton action="cancel" onclick={() => (dialogOpen = false)} disabled={creating} />
		<ArcaneButton
			action="create"
			customLabel={m.back_up_now()}
			onclick={handleCreate}
			loading={creating}
			disabled={!options || creating || (needsS3Destination && !s3DestinationId)}
			data-testid="project-backup-submit"
		/>
	{/snippet}
</ResponsiveDialog>

{#snippet ScheduleVolumes()}
	{#if scheduleOptionsQuery.isPending}
		<div class="flex items-center gap-2 text-sm text-muted-foreground">
			<Spinner class="size-4" />
			<span>{m.common_loading()}</span>
		</div>
	{:else if scheduleOptionsQuery.error}
		<Alert.Root variant="destructive" size="sm">
			<AlertIcon class="size-4" />
			<Alert.Description>{extractApiErrorMessage(scheduleOptionsQuery.error)}</Alert.Description>
		</Alert.Root>
	{:else if scheduleOptionsQuery.data?.volumes.length === 0}
		<p class="text-sm text-muted-foreground">{m.no_volumes_files_only()}</p>
	{:else}
		<div class="min-w-0 space-y-2">
			<p class="text-sm font-medium">{m.include_volume_data()}</p>
			{#if scheduleFollowsProject}
				<p class="text-xs text-muted-foreground">{m.volumes_added_later_included()}</p>
			{/if}
			<ProjectVolumeChecklist
				volumes={scheduleOptionsQuery.data?.volumes ?? []}
				bind:overrides={scheduleOverrides}
				selectable={backupSelectable}
				checkedByDefault={scheduleDefault}
				notes={backupVolumeNotes}
				idPrefix="project-backup-policy-volume"
			/>
		</div>
	{/if}
{/snippet}

{#if scheduleSession > 0 && policiesLoaded}
	{#key scheduleSession}
		<BackupPolicyDialog
			bind:open={scheduleOpen}
			idPrefix="project-backup-policy"
			{policies}
			policyId={editingPolicyId}
			addTitle={m.volume_backup_add_schedule()}
			description={m.back_up_on_schedule_keep_newest()}
			enabledDescription={m.back_up_project_on_schedule()}
			defaultSchedule="0 2 * * *"
			showStopContainers
			stopContainersDescription={m.stop_containers_during_snapshot()}
			retentionDescription={m.keep_newest_successful_schedule_backups()}
			defaultStopContainers
			destinations={canListS3Destinations && !s3DestinationsError ? s3Destinations : undefined}
			afterFields={ScheduleVolumes}
			policyPayload={(policy): UpdateProjectBackupPolicy => ({
				...backupPolicyUpdateFromPolicy(policy, true),
				volumes: policy.volumes
			})}
			extendUpdate={(update): UpdateProjectBackupPolicy => ({ ...update, volumes: scheduleVolumes() })}
			updatePolicies={async (next) => {
				if (!policiesLoaded) throw new Error(m.failed_to_save_backup_schedules());
				return (await projectService.updateBackupPolicies(projectId, next, environmentId)).policies;
			}}
			messages={{
				saved: m.backup_schedules_saved(),
				saveFailed: m.failed_to_save_backup_schedules(),
				removed: m.backup_schedule_removed()
			}}
			onSaved={(saved) =>
				queryClient.setQueryData(projectBackupPoliciesQuery(environmentId, projectId).queryKey, (current) => ({
					s3Available: current?.s3Available ?? false,
					policies: saved
				}))}
		/>
	{/key}
{/if}

<ResponsiveDialog
	bind:open={restoreOpen}
	title={m.restore_from_backup()}
	description={m.restore_backup_description()}
	contentClass="sm:max-w-xl"
	dismissible={!restoring}
>
	<div class="grid min-w-0 gap-y-4 py-4" data-testid="project-restore-dialog">
		<Alert.Root variant="warning">
			<AlertIcon class="size-4" />
			<Alert.Description>{m.restore_replaces_current_data()}</Alert.Description>
		</Alert.Root>
		<div class="min-w-0 space-y-2">
			<p class="text-sm font-medium">{m.what_to_restore()}</p>
			<div class="flex min-w-0 items-start gap-3 rounded-xl border bg-card/10 p-3">
				<Checkbox
					id="project-restore-files"
					class="mt-0.5"
					checked={restoreFiles}
					disabled={restoring || !restoreItem?.files || !canRestoreFiles}
					onCheckedChange={(checked) => (restoreFiles = checked === true)}
				/>
				<Label for="project-restore-files" class="mb-0">{m.project_files()}</Label>
			</div>
			<ProjectVolumeChecklist
				volumes={restoreVolumes}
				bind:overrides={restoreOverrides}
				selectable={() => true}
				idPrefix="project-restore-volume"
				disabled={restoring}
			/>
		</div>
		{#if running}
			<LabeledSwitch
				id="project-restore-redeploy"
				checked={redeploy}
				label={m.redeploy_after_restore()}
				description={m.redeploy_after_restore_description()}
				disabled={restoring}
				onCheckedChange={(next) => (redeploy = next)}
			/>
		{/if}
	</div>

	{#snippet footer()}
		<ArcaneButton action="cancel" onclick={() => (restoreOpen = false)} disabled={restoring} />
		<ArcaneButton
			action="confirm"
			customLabel={m.volumes_backups_restore()}
			onclick={handleRestore}
			loading={restoring}
			disabled={restoring || (!restoreFiles && selectedRestoreVolumes.length === 0)}
			data-testid="project-restore-submit"
		/>
	{/snippet}
</ResponsiveDialog>

<ProjectRestoreAsNewDialog bind:open={restoreAsNewOpen} backup={restoreItem} {environmentId} />
