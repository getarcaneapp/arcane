<script lang="ts">
	import { m } from '#lib/paraglide/messages';
	import { volumeBackupService, type VolumeBackupListResponse } from '#lib/services/volume-backup-service';
	import { s3DestinationService } from '#lib/services/s3-destination-service';
	import { volumeService } from '#lib/services/volume-service';
	import type { BackupEntry, CreateVolumeBackupRequest, VolumeBackupPolicy } from '#lib/types/shared';
	import type { S3Destination } from '#lib/types/s3-destination';
	import { onMount } from 'svelte';
	import {
		TrashIcon,
		AddIcon,
		ClockIcon,
		VolumesIcon,
		InfoIcon,
		DownloadIcon,
		RestartIcon,
		FileTextIcon,
		AlertIcon,
		UploadIcon,
		ArrowDownIcon
	} from '#lib/icons';
	import { ArcaneButton, arcaneButtonVariants } from '#lib/components/arcane-button';
	import * as ButtonGroup from '#lib/components/ui/button-group';
	import { toast } from 'svelte-sonner';
	import { bytes, formatDateTimeShort } from '#lib/utils/formatting';
	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import type { SearchPaginationSortRequest } from '#lib/types/shared';
	import {
		UniversalMobileCard,
		type BulkAction,
		type ColumnSpec,
		type MobileFieldVisibility
	} from '#lib/components/arcane-table';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu';
	import RowActionsMenu from '#lib/components/file-browser/row-actions-menu.svelte';
	import { openConfirmDialog } from '#lib/components/confirm-dialog';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog';
	import * as Alert from '#lib/components/ui/alert';
	import BackupFilePicker from '#lib/components/backup-file-picker.svelte';
	import { environmentStore } from '#lib/stores/environment.store.svelte';
	import { hasPermission } from '#lib/utils/auth';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast';
	import BackupPolicyDialog from '#lib/components/backup-policy-dialog.svelte';
	import BackupPolicyCard from '#lib/components/backup-policy-card.svelte';
	import BackupStatusCell from '#lib/components/arcane-table/cells/backup-status-cell.svelte';
	import BackupTriggerCell from '#lib/components/arcane-table/cells/backup-trigger-cell.svelte';
	import BackupDestinationCell from '#lib/components/arcane-table/cells/backup-destination-cell.svelte';
	import BackupSizeCell from '#lib/components/arcane-table/cells/backup-size-cell.svelte';
	import CreatedAtCell from '#lib/components/arcane-table/cells/created-at-cell.svelte';
	import { cn } from '#lib/utils';
	import { bulkConfirmAndRun } from '#lib/utils/bulk-actions';
	import { extractApiErrorMessage } from '#lib/utils/api';
	import {
		backupDestinationDisplay,
		backupTriggerLabel,
		s3DestinationOptions as buildS3DestinationOptions
	} from '#lib/utils/backups';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import type { BackupFileProvider } from '#lib/types/backup';

	type WorkspaceRestoreSelection = {
		paths: string[];
		selectAll: boolean;
	};

	let {
		volumeName,
		onWorkspaceRestored
	}: {
		volumeName: string;
		onWorkspaceRestored?: (selection: WorkspaceRestoreSelection) => void | Promise<void>;
	} = $props();

	const currentEnvId = $derived(environmentStore.selected?.id || '0');
	const canBackupVolume = $derived(hasPermission('volumes:backup', currentEnvId));
	const canDeleteBackup = $derived(hasPermission('volumes:backup', currentEnvId));

	let backupsPaginated = $state<VolumeBackupListResponse>({
		data: [],
		pagination: {
			currentPage: 1,
			totalPages: 1,
			totalItems: 0,
			itemsPerPage: 10
		}
	});
	let backupWarnings = $state<string[]>([]);
	let backupPolicies = $state<VolumeBackupPolicy[]>([]);
	let s3Destinations = $state<S3Destination[]>([]);
	let showBackupPolicy = $state(false);
	let editingBackupPolicyId = $state<string | undefined>();
	let showS3DestinationDialog = $state(false);
	let onDemandDestination = $state<'s3' | 'local_s3'>('s3');
	let onDemandS3DestinationId = $state('');
	const s3DestinationOptions = $derived(buildS3DestinationOptions(s3Destinations));

	let requestOptions = $state<SearchPaginationSortRequest>({
		pagination: { page: 1, limit: 10 },
		sort: { column: 'createdAt', direction: 'desc' }
	});

	let creating = $state(false);
	let deletingSelected = $state(false);
	let selectedIds = $state<string[]>([]);
	let uploadingBackupId = $state<string | null>(null);
	let restoringFiles = $state(false);
	let showRestoreFiles = $state(false);
	let restoreTarget = $state<BackupEntry | null>(null);
	let backupFileProvider = $state<BackupFileProvider | null>(null);
	let backupFilesSearch = $state('');
	let selectedPaths = $state<string[]>([]);
	let selectAllBackupFiles = $state(false);
	async function loadData(options: SearchPaginationSortRequest): Promise<VolumeBackupListResponse> {
		try {
			const result = await volumeBackupService.listBackups(volumeName, options);
			backupsPaginated = result;
			backupWarnings = result.warnings ?? [];
			return result;
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.volumes_backup_load_failed());
			return backupsPaginated;
		}
	}

	async function handleCreate(request?: CreateVolumeBackupRequest) {
		creating = true;
		try {
			const result = await volumeBackupService.createBackup(volumeName, request);
			toast.success(m.common_success(), activityToastOptions(extractActivityId(result)));
			await loadData(requestOptions);
			return true;
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.common_failed());
			return false;
		} finally {
			creating = false;
		}
	}

	function openS3DestinationDialog(destination: 's3' | 'local_s3') {
		onDemandDestination = destination;
		onDemandS3DestinationId = s3Destinations.length === 1 ? (s3Destinations[0]?.id ?? '') : '';
		showS3DestinationDialog = true;
	}

	async function createS3Backup() {
		if (!onDemandS3DestinationId) return;
		const created = await handleCreate({
			destination: onDemandDestination,
			s3DestinationId: onDemandS3DestinationId
		});
		if (created) {
			showS3DestinationDialog = false;
			onDemandS3DestinationId = '';
		}
	}

	async function handleDelete(backup: BackupEntry) {
		openConfirmDialog({
			title: m.common_remove_title({ resource: m.volumes_workspace_backup() }),
			message: m.volumes_backup_delete_confirm(),
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
					try {
						const result = await volumeBackupService.deleteBackup(backup.id);
						toast.success(
							m.common_delete_success({ resource: m.volumes_workspace_backup() }),
							activityToastOptions(extractActivityId(result))
						);
						await loadData(requestOptions);
					} catch (error) {
						toast.error(extractApiErrorMessage(error));
						await loadData(requestOptions);
					}
				}
			}
		});
	}

	function handleDeleteSelected(ids: string[]) {
		bulkConfirmAndRun({
			ids,
			title: m.volume_backups_remove_selected_title({ count: ids.length }),
			message: m.volume_backups_remove_selected_message({ count: ids.length }),
			confirmLabel: m.common_remove(),
			destructive: true,
			run: (id) => volumeBackupService.deleteBackup(id),
			messages: {
				success: (count) => m.common_bulk_remove_success({ count, resource: m.volumes_backups_title() }),
				partial: (success, total, failed) =>
					m.common_bulk_remove_partial({ success, total, failed, resource: m.volumes_backups_title() }),
				failure: () => m.common_bulk_remove_failed({ count: ids.length, resource: m.volumes_backups_title() })
			},
			setLoading: (loading) => (deletingSelected = loading),
			onItemFailure: (_id, error) => toast.error(extractApiErrorMessage(error)),
			onComplete: () => loadData(requestOptions),
			clearSelection: () => (selectedIds = [])
		});
	}

	async function handleUpload(backup: BackupEntry, s3DestinationId: string) {
		uploadingBackupId = backup.id;
		try {
			const result = await volumeBackupService.uploadBackup(backup.id, s3DestinationId);
			toast.success(m.backups_upload_s3_success(), activityToastOptions(extractActivityId(result)));
			await loadData(requestOptions);
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.backups_upload_s3_failed());
		} finally {
			uploadingBackupId = null;
		}
	}

	function openRestoreFilesDialog(backup: BackupEntry) {
		restoreTarget = backup;
		selectedPaths = [];
		selectAllBackupFiles = false;
		backupFilesSearch = '';
		backupFileProvider = {
			browse: (request) => volumeBackupService.browseBackupFiles(backup.id, request)
		};
		showRestoreFiles = true;
	}

	async function handleRestore(backup: BackupEntry) {
		// Check if volume is in use
		let usageWarning = '';
		try {
			const usage = await volumeService.getVolumeUsage(volumeName);
			if (usage.inUse && usage.containers?.length > 0) {
				usageWarning = m.volumes_backup_restore_in_use_warning({ count: usage.containers.length });
			}
		} catch {
			// Ignore errors checking usage
		}

		openConfirmDialog({
			title: m.volumes_backup_restore_title(),
			message: m.volumes_backup_restore_message({ volumeName }) + usageWarning,
			confirm: {
				label: m.volumes_backups_restore(),
				destructive: !!usageWarning,
				action: async () => {
					try {
						const result = await volumeBackupService.restoreBackup(volumeName, backup.id);
						await onWorkspaceRestored?.({ paths: [], selectAll: true });
						toast.success(m.volumes_backup_restore_success(), activityToastOptions(extractActivityId(result)));
						await loadData(requestOptions);
					} catch (error) {
						toast.error(error instanceof Error ? error.message : m.common_failed());
					}
				}
			}
		});
	}

	async function handleRestoreFiles() {
		if (!restoreTarget) return;
		if (!selectAllBackupFiles && !selectedPaths.length) return;

		const selection = { paths: [...selectedPaths], selectAll: selectAllBackupFiles } satisfies WorkspaceRestoreSelection;
		restoringFiles = true;
		try {
			const result = await volumeBackupService.restoreBackupFiles(volumeName, restoreTarget.id, {
				...selection,
				search: selection.selectAll ? backupFilesSearch.trim() : undefined
			});
			await onWorkspaceRestored?.(selection);
			toast.success(m.volumes_backup_restore_selection_success(), activityToastOptions(extractActivityId(result)));
			showRestoreFiles = false;
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.common_failed());
		} finally {
			restoringFiles = false;
		}
	}

	function formatBytes(value: number): string {
		return bytes.format(value, { unitSeparator: ' ' }) ?? '-';
	}

	onMount(async () => {
		const [collection, destinations] = await Promise.all([
			volumeBackupService.getPolicies(volumeName),
			canBackupVolume ? s3DestinationService.listAll().catch(() => []) : Promise.resolve([]),
			loadData(requestOptions)
		]);
		backupPolicies = collection.policies;
		s3Destinations = destinations;
	});

	const columns = [
		{ accessorKey: 'id', title: m.common_id(), sortable: true, cell: IdCell },
		{ accessorKey: 'status', title: m.common_status(), sortable: true, cell: StatusCell },
		{ accessorKey: 'trigger', title: m.volume_backup_trigger(), sortable: true, cell: TriggerCell },
		{ accessorKey: 'destination', title: m.backups_destination_label(), sortable: true, cell: DestinationCell },
		{ accessorKey: 'size', title: m.common_size(), sortable: true, cell: SizeCell },
		{ accessorKey: 'createdAt', title: m.common_created(), sortable: true, cell: CreatedCell },
		{
			accessorKey: 'remoteSnapshotId',
			title: m.volume_backup_remote_snapshot(),
			sortable: true,
			cell: RemoteSnapshotCell,
			hidden: true
		},
		{ accessorKey: 'error', title: m.common_error(), sortable: false, cell: ErrorCell, hidden: true }
	] satisfies ColumnSpec<BackupEntry>[];

	const mobileFields = [
		{ id: 'status', label: m.common_status(), defaultVisible: true },
		{ id: 'trigger', label: m.volume_backup_trigger(), defaultVisible: true },
		{ id: 'destination', label: m.backups_destination_label(), defaultVisible: true },
		{ id: 'size', label: m.common_size(), defaultVisible: true },
		{ id: 'remoteSnapshotId', label: m.volume_backup_remote_snapshot(), defaultVisible: false }
	];

	const bulkActions = $derived.by<BulkAction[]>(() => [
		{
			id: 'remove',
			label: m.common_remove_selected_count({ count: selectedIds.length }),
			action: 'remove',
			onClick: handleDeleteSelected,
			loading: deletingSelected,
			disabled: !canDeleteBackup || deletingSelected || selectedIds.length === 0,
			icon: TrashIcon
		}
	]);

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet IdCell({ item }: { item: BackupEntry })}
	<code class="font-mono text-xs font-medium">{item.id}</code>
{/snippet}

{#snippet StatusCell({ item }: { item: BackupEntry })}
	<BackupStatusCell status={item.status} />
{/snippet}

{#snippet TriggerCell({ item }: { item: BackupEntry })}
	<BackupTriggerCell trigger={item.trigger} />
{/snippet}

{#snippet DestinationCell({ item }: { item: BackupEntry })}
	<BackupDestinationCell {item} />
{/snippet}

{#snippet SizeCell({ item }: { item: BackupEntry })}
	<BackupSizeCell size={item.size} />
{/snippet}

{#snippet CreatedCell({ item }: { item: BackupEntry })}
	<CreatedAtCell value={item.createdAt} />
{/snippet}

{#snippet RemoteSnapshotCell({ item }: { item: BackupEntry })}
	<code class="text-xs">{item.remoteSnapshotId || m.volume_backup_not_uploaded()}</code>
{/snippet}

{#snippet ErrorCell({ item }: { item: BackupEntry })}
	<span class="line-clamp-2 max-w-80 text-xs text-destructive">{item.error || '-'}</span>
{/snippet}

{#snippet RowActions({ item }: { item: BackupEntry })}
	<RowActionsMenu openMenuLabel={m.common_open_menu()}>
		{#if canBackupVolume}
			<DropdownMenu.Item onclick={() => handleRestore(item)}>
				<RestartIcon class="size-4" />
				{m.volumes_backups_restore()}
			</DropdownMenu.Item>
			<DropdownMenu.Item onclick={() => openRestoreFilesDialog(item)}>
				<FileTextIcon class="size-4" />
				{m.volume_restore_files()}
			</DropdownMenu.Item>
		{/if}
		{#if item.format === 'archive' || item.localSnapshotId}
			<DropdownMenu.Item onclick={() => volumeBackupService.downloadBackup(item.id)}>
				<DownloadIcon class="size-4" />
				{m.templates_download()}
			</DropdownMenu.Item>
		{/if}
		{#if canBackupVolume && s3Destinations.length}
			{#each s3Destinations as destination (destination.id)}
				<DropdownMenu.Item
					disabled={item.status !== 'succeeded' || Boolean(item.remoteSnapshotId) || uploadingBackupId === item.id}
					onclick={() => handleUpload(item, destination.id)}
				>
					<UploadIcon class="size-4" />
					{m.backups_upload_s3()} · {destination.name}
				</DropdownMenu.Item>
			{/each}
		{/if}
		<IfPermitted perm="volumes:backup">
			<DropdownMenu.Separator />
			<DropdownMenu.Item variant="destructive" onclick={() => handleDelete(item)}>
				<TrashIcon class="size-4" />
				{m.common_remove()}
			</DropdownMenu.Item>
		</IfPermitted>
	</RowActionsMenu>
{/snippet}

{#snippet ToolbarActions()}
	{#if canBackupVolume}
		<div class="flex items-center gap-2">
			<ArcaneButton
				action="create"
				customLabel={m.volume_backup_add_schedule()}
				onclick={() => {
					editingBackupPolicyId = undefined;
					showBackupPolicy = true;
				}}
				size="sm"
				icon={AddIcon}
			/>
			<ButtonGroup.Root>
				<ArcaneButton
					action="create"
					customLabel={m.volumes_backup_create()}
					loading={creating}
					disabled={creating}
					onclick={() => handleCreate()}
					size="sm"
					icon={AddIcon}
				/>
				<DropdownMenu.Root>
					<DropdownMenu.Trigger
						class={cn(arcaneButtonVariants({ tone: 'outline-primary', size: 'icon' }), 'size-8 rounded-md')}
						aria-label={m.common_open_menu()}
						disabled={creating}
					>
						<ArrowDownIcon class="size-4" />
					</DropdownMenu.Trigger>
					<DropdownMenu.Content align="end" class="w-64">
						<DropdownMenu.Label>{m.backups_destination_label()}</DropdownMenu.Label>
						<DropdownMenu.Item onclick={() => handleCreate({ destination: 'local' })}>
							{m.local()}
						</DropdownMenu.Item>
						<DropdownMenu.Item onclick={() => openS3DestinationDialog('s3')}>
							{m.backups_destination_s3()}
						</DropdownMenu.Item>
						<DropdownMenu.Item onclick={() => openS3DestinationDialog('local_s3')}>
							{m.backups_destination_local_s3()}
						</DropdownMenu.Item>
					</DropdownMenu.Content>
				</DropdownMenu.Root>
			</ButtonGroup.Root>
		</div>
	{/if}
{/snippet}

{#snippet BackupMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: BackupEntry;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={{ component: VolumesIcon, variant: 'blue' }}
		title={(item) => item.id}
		fields={[
			{
				label: m.volume_backup_trigger(),
				getValue: (item) => backupTriggerLabel(item.trigger),
				icon: ClockIcon,
				iconVariant: 'gray',
				show: mobileFieldVisibility['trigger'] ?? true
			},
			{
				label: m.common_size(),
				getValue: (item) => formatBytes(item.size),
				icon: InfoIcon,
				iconVariant: 'gray',
				show: mobileFieldVisibility['size'] ?? true
			},
			{
				label: m.backups_destination_label(),
				getValue: (item) => backupDestinationDisplay(item),
				icon: DownloadIcon,
				iconVariant: 'gray',
				show: mobileFieldVisibility['destination'] ?? true
			},
			{
				label: m.volume_backup_remote_snapshot(),
				getValue: (item) => item.remoteSnapshotId || m.volume_backup_not_uploaded(),
				icon: DownloadIcon,
				iconVariant: 'gray',
				show: mobileFieldVisibility['remoteSnapshotId'] ?? false
			}
		]}
		footer={{
			label: m.common_created(),
			getValue: (item) => formatDateTimeShort(item.createdAt),
			icon: ClockIcon
		}}
		rowActions={RowActions}
	/>
{/snippet}

<div class="space-y-4">
	<div class="flex items-center justify-between">
		<h2 class="text-lg font-semibold">{m.volumes_backups_title()}</h2>
	</div>
	<div class="grid grid-cols-1 gap-1.5 text-xs text-muted-foreground sm:grid-cols-2 xl:grid-cols-3">
		{#each backupPolicies as policy (policy.id)}
			<BackupPolicyCard
				{policy}
				showStopContainers
				onEdit={canBackupVolume
					? () => {
							editingBackupPolicyId = policy.id;
							showBackupPolicy = true;
						}
					: undefined}
			/>
		{/each}
	</div>

	<Alert.Root class="py-2 [&>svg]:top-2">
		<InfoIcon class="size-4" />
		<Alert.Description class="text-xs">
			{m.volume_backup_encryption_note()}
		</Alert.Description>
	</Alert.Root>

	{#if backupWarnings.length > 0}
		<Alert.Root variant="warning" class="py-2 [&>svg]:top-2">
			<AlertIcon class="size-4" />
			<Alert.Description class="text-xs">
				{backupWarnings[0]}
			</Alert.Description>
		</Alert.Root>
	{/if}

	<ArcaneTable
		persistKey="arcane-volume-backup-table"
		items={backupsPaginated}
		bind:selectedIds
		bind:requestOptions
		bind:mobileFieldVisibility
		onRefresh={loadData}
		{columns}
		{mobileFields}
		{bulkActions}
		rowActions={RowActions}
		mobileCard={BackupMobileCardSnippet}
		customToolbarActions={ToolbarActions}
	/>
</div>

<ResponsiveDialog
	bind:open={showRestoreFiles}
	onOpenChange={(open) => {
		if (!open) {
			backupFileProvider = null;
			selectedPaths = [];
			selectAllBackupFiles = false;
			backupFilesSearch = '';
		}
	}}
	title={m.volume_restore_files()}
	description={m.volumes_backup_restore_desc()}
	contentClass="sm:max-w-[640px]"
>
	{#snippet children()}
		<div class="space-y-3 py-2">
			<Alert.Root class="py-2 [&>svg]:top-2">
				<InfoIcon class="size-4" />
				<Alert.Description class="text-xs">
					{m.volume_backup_restore_files_lifecycle_info()}
				</Alert.Description>
			</Alert.Root>

			{#if backupFileProvider}
				<BackupFilePicker
					provider={backupFileProvider}
					bind:selectedPaths
					bind:selectAll={selectAllBackupFiles}
					bind:search={backupFilesSearch}
				/>
			{/if}

			<Alert.Root variant="warning" class="py-2 [&>svg]:top-2">
				<AlertIcon class="size-4" />
				<Alert.Description class="text-xs">
					{m.volumes_backup_overwrite_warning()}
				</Alert.Description>
			</Alert.Root>
		</div>
	{/snippet}

	{#snippet footer()}
		<ArcaneButton
			action="cancel"
			onclick={() => {
				showRestoreFiles = false;
				selectedPaths = [];
				selectAllBackupFiles = false;
				backupFilesSearch = '';
			}}
		/>
		{#if canBackupVolume}
			<ArcaneButton
				action="create"
				customLabel={m.volume_restore_files()}
				onclick={handleRestoreFiles}
				loading={restoringFiles}
				disabled={restoringFiles || (!selectAllBackupFiles && selectedPaths.length === 0)}
			/>
		{/if}
	{/snippet}
</ResponsiveDialog>

<ResponsiveDialog
	bind:open={showS3DestinationDialog}
	title={m.volume_backup_choose_s3_destination()}
	description={m.volume_backup_choose_s3_destination_description()}
	contentClass="sm:max-w-[520px]"
>
	{#snippet children()}
		<div class="py-2">
			<SelectWithLabel
				id="on-demand-volume-backup-s3-destination"
				value={onDemandS3DestinationId}
				onValueChange={(value) => (onDemandS3DestinationId = value)}
				label={m.volume_backup_s3_destination_label()}
				description={m.volume_backup_s3_destination_description()}
				options={s3DestinationOptions}
			/>
			{#if s3Destinations.length === 0}
				<p class="mt-2 text-xs text-muted-foreground">{m.volume_backup_no_s3_destinations()}</p>
			{/if}
		</div>
	{/snippet}
	{#snippet footer()}
		<ArcaneButton action="cancel" onclick={() => (showS3DestinationDialog = false)} disabled={creating} />
		<ArcaneButton
			action="create"
			customLabel={m.volumes_backup_create()}
			onclick={createS3Backup}
			loading={creating}
			disabled={creating || !onDemandS3DestinationId}
		/>
	{/snippet}
</ResponsiveDialog>

<BackupPolicyDialog
	bind:open={showBackupPolicy}
	idPrefix="volume-backup-policy"
	policies={backupPolicies}
	policyId={editingBackupPolicyId}
	addTitle={m.volume_backup_add_schedule()}
	description={m.volume_backup_policy_description()}
	enabledDescription={m.volume_backup_policy_enabled_description()}
	defaultSchedule="0 0 2 * * *"
	showStopContainers
	updatePolicies={async (policies) => (await volumeBackupService.updatePolicies(volumeName, policies)).policies}
	messages={{
		saved: m.volume_backup_policy_saved(),
		saveFailed: m.volume_backup_policy_save_failed(),
		removed: m.volume_backup_schedule_removed()
	}}
	onSaved={(policies) => (backupPolicies = policies)}
/>
