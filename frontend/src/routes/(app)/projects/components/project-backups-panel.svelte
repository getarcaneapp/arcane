<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import BackupDestinationCell from '#lib/components/arcane-table/cells/backup-destination-cell.svelte';
	import BackupSizeCell from '#lib/components/arcane-table/cells/backup-size-cell.svelte';
	import BackupStatusCell from '#lib/components/arcane-table/cells/backup-status-cell.svelte';
	import BackupTriggerCell from '#lib/components/arcane-table/cells/backup-trigger-cell.svelte';
	import CreatedAtCell from '#lib/components/arcane-table/cells/created-at-cell.svelte';
	import { UniversalMobileCard, type ColumnSpec, type MobileFieldVisibility } from '#lib/components/arcane-table/index.js';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import RowActionsMenu from '#lib/components/file-browser/row-actions-menu.svelte';
	import LabeledSwitch from '#lib/components/form/labeled-switch.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { useBackupActivity } from '#lib/hooks/use-backup-activity.svelte.js';
	import {
		AddIcon,
		AlertIcon,
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
	import { projectBackupListQuery } from '#lib/query/projects.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { projectService } from '#lib/services/project-service.js';
	import { activityStore } from '#lib/stores/activity.store.svelte.js';
	import type { S3Destination, SystemBackupDestination } from '#lib/types/backup.js';
	import type { Paginated } from '#lib/types/base.js';
	import type { ProjectBackup, ProjectExportOptions, ProjectVolume } from '#lib/types/project.js';
	import { GLOBAL_SCOPE } from '#lib/types/role.js';
	import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
	import { activityToastOptions } from '#lib/utils/activity-toast.js';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import {
		backupDestinationDisplay,
		backupDestinationOptions,
		backupStatusLabel,
		backupTriggerLabel,
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
		onRestored?: (restored: { files: boolean }) => Promise<unknown>;
	} = $props();

	const canBackup = $derived(hasPermission('projects:backup', environmentId));
	const canRestore = $derived(hasPermission('projects:restore', environmentId));
	const canRestoreAsNew = $derived(hasPermission('projects:create', environmentId));
	const canStartRestored = $derived(hasPermission('projects:deploy', environmentId));
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
	let restoreName = $state('');
	let startRestored = $state(false);
	let restoring = $state(false);

	const selectedVolumes = $derived(
		(options?.volumes ?? []).filter((volume) => volume.included && (checkedOverrides[volume.key] ?? true))
	);
	const needsS3Destination = $derived(destination !== 'local');
	const restoreVolumes = $derived((restoreItem?.volumes ?? []).filter((volume) => volume.included));
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
		return queryClient.invalidateQueries({ queryKey: queryKeys.projects.backupListPrefix(environmentId, projectId) });
	}

	const backupActivity = useBackupActivity(
		() => environmentId,
		(activity) => activity.metadata?.['action'] === 'create_project_backup' && activity.resourceId === projectId,
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
	$effect(() => {
		const restoring = restoreActivity.activeIds.length > 0;
		if (restoreWasRunning && !restoring) {
			void onRestored?.({ files: restoringFiles });
			restoringFiles = false;
		}
		restoreWasRunning = restoring;
	});

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
		restoreFiles = true;
		restoreOverrides = {};
		redeploy = true;
		restoreName = `${backup.projectName}-restored`;
		startRestored = false;
	}

	function openRestore(backup: ProjectBackup) {
		resetRestore(backup);
		restoreOpen = true;
	}

	function openRestoreAsNew(backup: ProjectBackup) {
		resetRestore(backup);
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

	async function handleRestoreAsNew() {
		if (!restoreItem || restoring || !restoreName.trim()) return;
		restoring = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(
				projectService.restoreBackupAsNew(
					projectId,
					restoreItem.id,
					{
						name: restoreName.trim(),
						volumes: selectedRestoreVolumes.map((volume) => volume.key),
						start: startRestored && canStartRestored
					},
					environmentId
				)
			),
			message: m.failed_to_restore_backup(),
			setLoadingState: (value) => (restoring = value),
			onSuccess: async (project) => {
				restoreAsNewOpen = false;
				toast.success(m.project_restored_from_backup({ name: project.name }), activityToastOptions(project.activityId));
				await goto(resolve(`/(app)/projects/[projectId]?env=${encodeURIComponent(environmentId)}`, { projectId: project.id }));
			}
		});
	}

	function volumeNote(volume: ProjectVolume): string | null {
		if (volume.backupStore) return m.backup_store_not_backed_up();
		if (volume.missing) return m.not_created_yet_nothing_to_back_up();
		if (volume.kind === 'driverOpts') return m.driver_options_not_backed_up();
		if (volume.kind === 'external' && !volume.included) return m.external_volume_not_backed_up();
		if (volume.inUse) return m.volume_in_use_elsewhere();
		return null;
	}

	function includedVolumes(backup: ProjectBackup): string {
		const keys = backup.volumes.filter((volume) => volume.included).map((volume) => volume.key);
		return keys.length ? keys.join(', ') : m.files_only();
	}

	const columns = [
		{ accessorKey: 'createdAt', title: m.common_created(), cell: CreatedCell },
		{ accessorKey: 'status', title: m.common_status(), cell: StatusCell },
		{ accessorKey: 'trigger', title: m.volume_backup_trigger(), cell: TriggerCell },
		{ accessorKey: 'volumes', title: m.resource_volumes_cap(), cell: VolumesCell },
		{ accessorKey: 'destination', title: m.backups_destination_label(), cell: DestinationCell },
		{ accessorKey: 'size', title: m.common_size(), cell: SizeCell },
		{ accessorKey: 'error', title: m.common_error(), cell: ErrorCell, hidden: true }
	] satisfies ColumnSpec<ProjectBackup>[];

	const mobileFields = [
		{ id: 'status', label: m.common_status(), defaultVisible: true },
		{ id: 'trigger', label: m.volume_backup_trigger(), defaultVisible: true },
		{ id: 'volumes', label: m.resource_volumes_cap(), defaultVisible: true },
		{ id: 'destination', label: m.backups_destination_label(), defaultVisible: true },
		{ id: 'size', label: m.common_size(), defaultVisible: true }
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
	<span class="line-clamp-2 max-w-80 text-xs text-destructive">{item.error || '-'}</span>
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
			{#if canRestoreAsNew}
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

{#snippet RestoreParts(filesRequired: boolean)}
	<div class="min-w-0 space-y-2">
		<p class="text-sm font-medium">{m.what_to_restore()}</p>
		<ul class="space-y-2">
			<li class="flex min-w-0 items-start gap-3 rounded-xl border bg-card/10 p-3">
				<Checkbox
					id="project-restore-files"
					class="mt-0.5"
					checked={filesRequired || restoreFiles}
					disabled={filesRequired || restoring}
					onCheckedChange={(checked) => (restoreFiles = checked === true)}
				/>
				<Label for="project-restore-files" class="mb-0">{m.project_files()}</Label>
			</li>
			{#each restoreVolumes as volume (volume.key)}
				<li class="flex min-w-0 items-start gap-3 rounded-xl border bg-card/10 p-3">
					<Checkbox
						id={`project-restore-volume-${volume.key}`}
						class="mt-0.5"
						checked={restoreOverrides[volume.key] ?? true}
						disabled={restoring}
						onCheckedChange={(checked) => (restoreOverrides = { ...restoreOverrides, [volume.key]: checked === true })}
					/>
					<div class="grid min-w-0 gap-1">
						<Label for={`project-restore-volume-${volume.key}`} class="mb-0 break-all">{volume.key}</Label>
						<span class="font-mono text-xs break-all text-muted-foreground">{volume.name}</span>
					</div>
				</li>
			{/each}
		</ul>
	</div>
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
			}
		]}
		footer={{ label: m.common_created(), getValue: (item) => formatDateTimeShort(item.createdAt), icon: ClockIcon }}
		rowActions={RowActions}
	/>
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
					<ul class="space-y-2">
						{#each options.volumes as volume (volume.key)}
							{@const note = volumeNote(volume)}
							<li class="flex min-w-0 items-start gap-3 rounded-xl border bg-card/10 p-3">
								<Checkbox
									id={`project-backup-volume-${volume.key}`}
									class="mt-0.5"
									checked={volume.included && (checkedOverrides[volume.key] ?? true)}
									disabled={!volume.included || creating}
									onCheckedChange={(checked) => (checkedOverrides = { ...checkedOverrides, [volume.key]: checked === true })}
								/>
								<div class="grid min-w-0 gap-1">
									<Label for={`project-backup-volume-${volume.key}`} class="mb-0 break-all">{volume.key}</Label>
									<span class="font-mono text-xs break-all text-muted-foreground">{volume.name}</span>
									{#if note}
										<span class={['text-xs', volume.inUse ? 'text-warning' : 'text-muted-foreground']}>{note}</span>
									{/if}
								</div>
							</li>
						{/each}
					</ul>
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
		{@render RestoreParts(false)}
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

<ResponsiveDialog
	bind:open={restoreAsNewOpen}
	title={m.restore_as_new_project()}
	description={m.restore_as_new_project_description()}
	contentClass="sm:max-w-xl"
	dismissible={!restoring}
>
	<div class="grid min-w-0 gap-y-4 py-4" data-testid="project-restore-as-new-dialog">
		<div class="space-y-2">
			<Label for="project-restore-name">{m.project_name()}</Label>
			<Input
				id="project-restore-name"
				value={restoreName}
				autocomplete="off"
				disabled={restoring}
				oninput={(event) => (restoreName = event.currentTarget.value)}
			/>
		</div>
		{@render RestoreParts(true)}
		<LabeledSwitch
			id="project-restore-start"
			checked={startRestored && canStartRestored}
			label={m.start_after_restore()}
			disabled={!canStartRestored || restoring}
			onCheckedChange={(next) => (startRestored = next)}
		/>
	</div>

	{#snippet footer()}
		<ArcaneButton action="cancel" onclick={() => (restoreAsNewOpen = false)} disabled={restoring} />
		<ArcaneButton
			action="create"
			customLabel={m.restore_as_new_project()}
			onclick={handleRestoreAsNew}
			loading={restoring}
			disabled={restoring || !restoreName.trim()}
			data-testid="project-restore-as-new-submit"
		/>
	{/snippet}
</ResponsiveDialog>
