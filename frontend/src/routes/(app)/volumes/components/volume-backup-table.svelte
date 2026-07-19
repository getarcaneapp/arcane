<script lang="ts">
	import { m } from '$lib/paraglide/messages';
	import { volumeBackupService, type VolumeBackupListResponse } from '$lib/services/volume-backup-service';
	import { volumeService } from '$lib/services/volume-service';
	import type { BackupEntry, VolumeBackupDestination, VolumeBackupPolicy } from '$lib/types/shared';
	import { onMount } from 'svelte';
	import {
		LoadingSpinnerIcon,
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
	} from '$lib/icons';
	import { ArcaneButton, arcaneButtonVariants } from '$lib/components/arcane-button';
	import * as ButtonGroup from '$lib/components/ui/button-group';
	import { toast } from 'svelte-sonner';
	import { bytes, formatDateTimeShort } from '$lib/utils/formatting';
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import type { SearchPaginationSortRequest } from '$lib/types/shared';
	import { UniversalMobileCard, type ColumnSpec, type MobileFieldVisibility } from '$lib/components/arcane-table';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu';
	import RowActionsMenu from '$lib/components/file-browser/row-actions-menu.svelte';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog';
	import { Input } from '$lib/components/ui/input';
	import { ScrollArea } from '$lib/components/ui/scroll-area';
	import * as Checkbox from '$lib/components/ui/checkbox';
	import * as Alert from '$lib/components/ui/alert';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { hasPermission } from '$lib/utils/auth';
	import IfPermitted from '$lib/components/if-permitted.svelte';
	import { activityToastOptions, extractActivityId } from '$lib/utils/activity-toast';
	import VolumeBackupPolicyDialog from './volume-backup-policy-dialog.svelte';
	import { Badge } from '$lib/components/ui/badge';
	import { cn } from '$lib/utils';

	let { volumeName }: { volumeName: string } = $props();

	const currentEnvId = $derived(environmentStore.selected?.id || '0');
	const canBackupVolume = $derived(hasPermission('volumes:backup', currentEnvId));

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
	let backupPolicy = $state<VolumeBackupPolicy>({
		volumeName: '',
		enabled: false,
		schedule: '0 0 2 * * *',
		retentionCount: 7,
		stopContainers: false,
		localEnabled: true,
		s3Enabled: false,
		s3DestinationId: '',
		s3DestinationName: '',
		s3Available: false,
		s3Bucket: ''
	});
	let showBackupPolicy = $state(false);

	let requestOptions = $state<SearchPaginationSortRequest>({
		pagination: { page: 1, limit: 10 },
		sort: { column: 'createdAt', direction: 'desc' }
	});

	let creating = $state(false);
	let uploadingBackupId = $state<string | null>(null);
	let restoringFiles = $state(false);
	let showRestoreFiles = $state(false);
	let restoreTarget = $state<BackupEntry | null>(null);
	let backupFiles = $state<string[]>([]);
	let backupFilesLoading = $state(false);
	let backupFilesSearch = $state('');
	let selectedPaths = $state<string[]>([]);
	const filteredBackupFiles = $derived.by(() => {
		const q = backupFilesSearch.trim().toLowerCase();
		if (!q) return backupFiles;
		return backupFiles.filter((p) => p.toLowerCase().includes(q));
	});

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

	async function handleCreate(destination?: VolumeBackupDestination) {
		creating = true;
		try {
			const result = await volumeBackupService.createBackup(volumeName, destination);
			toast.success(m.common_success(), activityToastOptions(extractActivityId(result)));
			await loadData(requestOptions);
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.common_failed());
		} finally {
			creating = false;
		}
	}

	async function handleDelete(backup: BackupEntry) {
		openConfirmDialog({
			title: m.common_remove_title({ resource: m.file_browser_backup() }),
			message: m.volumes_backup_delete_confirm(),
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
					try {
						const result = await volumeBackupService.deleteBackup(backup.id);
						toast.success(
							m.common_delete_success({ resource: m.file_browser_backup() }),
							activityToastOptions(extractActivityId(result))
						);
						await loadData(requestOptions);
					} catch (error) {
						toast.error(error instanceof Error ? error.message : m.common_delete_failed({ resource: m.file_browser_backup() }));
					}
				}
			}
		});
	}

	async function handleUpload(backup: BackupEntry) {
		uploadingBackupId = backup.id;
		try {
			const result = await volumeBackupService.uploadBackup(backup.id);
			toast.success(m.backups_upload_s3_success(), activityToastOptions(extractActivityId(result)));
			await loadData(requestOptions);
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.backups_upload_s3_failed());
		} finally {
			uploadingBackupId = null;
		}
	}

	async function openRestoreFilesDialog(backup: BackupEntry) {
		restoreTarget = backup;
		selectedPaths = [];
		backupFiles = [];
		backupFilesSearch = '';
		showRestoreFiles = true;
		backupFilesLoading = true;
		try {
			backupFiles = await volumeBackupService.listBackupFiles(backup.id);
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.common_failed());
		} finally {
			backupFilesLoading = false;
		}
	}

	function togglePath(path: string, checked: boolean) {
		if (checked) {
			if (!selectedPaths.includes(path)) {
				selectedPaths = [...selectedPaths, path];
			}
			return;
		}
		selectedPaths = selectedPaths.filter((p) => p !== path);
	}

	function selectAllVisible() {
		const next = new Set(selectedPaths);
		for (const p of filteredBackupFiles) {
			next.add(p);
		}
		selectedPaths = Array.from(next);
	}

	function clearSelection() {
		selectedPaths = [];
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
		if (!selectedPaths.length) return;

		restoringFiles = true;
		try {
			const result = await volumeBackupService.restoreBackupFiles(volumeName, restoreTarget.id, selectedPaths);
			toast.success(
				m.volumes_backup_restore_files_success({ count: selectedPaths.length }),
				activityToastOptions(extractActivityId(result))
			);
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

	function backupStatusLabel(status: BackupEntry['status']) {
		if (status === 'succeeded') return m.volume_backup_status_succeeded();
		if (status === 'failed') return m.volume_backup_status_failed();
		return m.volume_backup_status_running();
	}

	function backupStatusVariant(status: BackupEntry['status']): 'green' | 'red' | 'blue' {
		if (status === 'succeeded') return 'green';
		if (status === 'failed') return 'red';
		return 'blue';
	}

	function backupTriggerLabel(trigger: BackupEntry['trigger']) {
		if (trigger === 'scheduled') return m.backups_trigger_scheduled();
		if (trigger === 'safety') return m.backups_trigger_safety();
		return m.backups_trigger_manual();
	}

	function backupDestinationLabel(destination: BackupEntry['destination']) {
		if (destination === 'local_s3') return m.backups_destination_local_s3();
		if (destination === 's3') return m.backups_destination_s3();
		return m.backups_destination_local();
	}

	function backupDestinationName(backup: BackupEntry) {
		return backup.s3DestinationName || backup.s3DestinationId || '';
	}

	function backupDestinationDisplay(backup: BackupEntry) {
		const label = backupDestinationLabel(backup.destination);
		const name = backupDestinationName(backup);
		return name && backup.destination !== 'local' ? `${label} · ${name}` : label;
	}

	const configuredS3DestinationName = $derived(
		backupPolicy.s3DestinationName || backupPolicy.s3Bucket || backupPolicy.s3DestinationId
	);

	onMount(async () => {
		const [policy] = await Promise.all([volumeBackupService.getPolicy(volumeName), loadData(requestOptions)]);
		backupPolicy = policy;
	});

	const columns = [
		{ accessorKey: 'id', title: m.common_id(), sortable: true, cell: IdCell },
		{ accessorKey: 'status', title: m.common_status(), sortable: true, cell: StatusCell },
		{ accessorKey: 'trigger', title: m.volume_backup_trigger(), sortable: true, cell: TriggerCell },
		{ accessorKey: 'destination', title: m.volume_backup_destination_label(), sortable: true, cell: DestinationCell },
		{ accessorKey: 'size', title: m.common_size(), sortable: true, cell: SizeCell },
		{ accessorKey: 'createdAt', title: m.common_created(), sortable: true, cell: CreatedCell },
		{ accessorKey: 'remoteKey', title: m.volume_backup_remote_key(), sortable: true, cell: RemoteKeyCell, hidden: true },
		{ accessorKey: 'error', title: m.common_error(), sortable: false, cell: ErrorCell, hidden: true }
	] satisfies ColumnSpec<BackupEntry>[];

	const mobileFields = [
		{ id: 'status', label: m.common_status(), defaultVisible: true },
		{ id: 'trigger', label: m.volume_backup_trigger(), defaultVisible: true },
		{ id: 'destination', label: m.volume_backup_destination_label(), defaultVisible: true },
		{ id: 'size', label: m.common_size(), defaultVisible: true },
		{ id: 'remoteKey', label: m.volume_backup_remote_key(), defaultVisible: false }
	];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet IdCell({ item }: { item: BackupEntry })}
	<code class="font-mono text-xs font-medium">{item.id}</code>
{/snippet}

{#snippet StatusCell({ item }: { item: BackupEntry })}
	<Badge variant={backupStatusVariant(item.status)}>{backupStatusLabel(item.status)}</Badge>
{/snippet}

{#snippet TriggerCell({ item }: { item: BackupEntry })}
	{backupTriggerLabel(item.trigger)}
{/snippet}

{#snippet DestinationCell({ item }: { item: BackupEntry })}
	<div class="flex items-center gap-2">
		<Badge variant={item.destination === 'local' ? 'gray' : 'blue'}>{backupDestinationLabel(item.destination)}</Badge>
		{#if item.destination !== 'local' && backupDestinationName(item)}
			<span class="max-w-48 truncate text-xs text-muted-foreground">{backupDestinationName(item)}</span>
		{/if}
	</div>
{/snippet}

{#snippet SizeCell({ item }: { item: BackupEntry })}
	{formatBytes(item.size)}
{/snippet}

{#snippet CreatedCell({ item }: { item: BackupEntry })}
	{formatDateTimeShort(item.createdAt)}
{/snippet}

{#snippet RemoteKeyCell({ item }: { item: BackupEntry })}
	<code class="text-xs">{item.remoteKey || m.volume_backup_not_uploaded()}</code>
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
		<DropdownMenu.Item onclick={() => volumeBackupService.downloadBackup(item.id)}>
			<DownloadIcon class="size-4" />
			{m.templates_download()}
		</DropdownMenu.Item>
		{#if canBackupVolume && backupPolicy.s3Enabled && backupPolicy.s3DestinationId}
			<DropdownMenu.Item
				disabled={item.status !== 'succeeded' || Boolean(item.remoteKey) || uploadingBackupId === item.id}
				onclick={() => handleUpload(item)}
			>
				<UploadIcon class="size-4" />
				{m.backups_upload_s3()}
			</DropdownMenu.Item>
		{/if}
		<IfPermitted perm="volumes:delete">
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
		<ArcaneButton
			action="base"
			customLabel={m.volume_backup_policy_configure()}
			onclick={() => (showBackupPolicy = true)}
			size="sm"
			icon={ClockIcon}
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
					<DropdownMenu.Label>{m.volume_backup_destination_label()}</DropdownMenu.Label>
					<DropdownMenu.Item onclick={() => handleCreate('local')}>
						{m.volume_backup_destination_local()}
					</DropdownMenu.Item>
					{#if backupPolicy.s3Enabled && backupPolicy.s3DestinationId}
						<DropdownMenu.Item onclick={() => handleCreate('local_s3')}>
							<div class="flex flex-col gap-0.5">
								<span>{m.volume_backup_destination_local_s3()}</span>
								<span class="text-xs text-muted-foreground">{configuredS3DestinationName}</span>
							</div>
						</DropdownMenu.Item>
						<DropdownMenu.Item onclick={() => handleCreate('s3')}>
							<div class="flex flex-col gap-0.5">
								<span>{m.volume_backup_destination_s3()}</span>
								<span class="text-xs text-muted-foreground">{configuredS3DestinationName}</span>
							</div>
						</DropdownMenu.Item>
					{/if}
				</DropdownMenu.Content>
			</DropdownMenu.Root>
		</ButtonGroup.Root>
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
				label: m.volume_backup_destination_label(),
				getValue: (item) => backupDestinationDisplay(item),
				icon: DownloadIcon,
				iconVariant: 'gray',
				show: mobileFieldVisibility['destination'] ?? true
			},
			{
				label: m.volume_backup_remote_key(),
				getValue: (item) => item.remoteKey || m.volume_backup_not_uploaded(),
				icon: DownloadIcon,
				iconVariant: 'gray',
				show: mobileFieldVisibility['remoteKey'] ?? false
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
	<div class="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
		<Badge variant={backupPolicy.enabled ? 'green' : 'gray'}>
			{backupPolicy.enabled ? m.common_enabled() : m.common_disabled()}
		</Badge>
		{#if backupPolicy.enabled}<code>{backupPolicy.schedule}</code>{/if}
		<Badge variant="blue">
			{backupPolicy.s3Enabled
				? backupPolicy.localEnabled
					? m.backups_destination_local_s3()
					: m.backups_destination_s3()
				: m.backups_destination_local()}
		</Badge>
	</div>

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
		bind:requestOptions
		bind:mobileFieldVisibility
		onRefresh={loadData}
		{columns}
		{mobileFields}
		rowActions={RowActions}
		mobileCard={BackupMobileCardSnippet}
		customToolbarActions={ToolbarActions}
	/>
</div>

<ResponsiveDialog
	bind:open={showRestoreFiles}
	title={m.volume_restore_files()}
	description={m.volumes_backup_restore_desc()}
	contentClass="sm:max-w-[640px]"
>
	{#snippet children()}
		<div class="space-y-3 py-2">
			<Alert.Root class="py-2 [&>svg]:top-2">
				<InfoIcon class="size-4" />
				<Alert.Description class="text-xs">
					{m.volumes_backup_safety_info()}
				</Alert.Description>
			</Alert.Root>

			<div class="flex items-center justify-between gap-2">
				<Input class="h-9" placeholder={m.volume_search_files()} bind:value={backupFilesSearch} />
				<div class="flex items-center gap-2">
					<ArcaneButton action="base" tone="ghost" size="sm" onclick={selectAllVisible} customLabel={m.common_select_all()} />
					<ArcaneButton action="base" tone="ghost" size="sm" onclick={clearSelection} customLabel={m.common_clear()} />
				</div>
			</div>

			<ScrollArea class="h-64 rounded-md border">
				{#if backupFilesLoading}
					<div class="flex items-center justify-center py-8">
						<LoadingSpinnerIcon class="size-5 text-muted-foreground" />
					</div>
				{:else if filteredBackupFiles.length === 0}
					<div class="flex items-center justify-center py-8 text-sm text-muted-foreground">{m.volume_backup_no_files()}</div>
				{:else}
					<div class="divide-y divide-border/40">
						{#each filteredBackupFiles as filePath (filePath)}
							<div class="flex items-center gap-3 px-3 py-2">
								<Checkbox.Root
									checked={selectedPaths.includes(filePath)}
									onCheckedChange={(value) => togglePath(filePath, !!value)}
								/>
								<code class="font-mono text-xs break-all">{filePath}</code>
							</div>
						{/each}
					</div>
				{/if}
			</ScrollArea>

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
				backupFilesSearch = '';
			}}
		/>
		{#if canBackupVolume}
			<ArcaneButton
				action="create"
				customLabel={m.volume_restore_files()}
				onclick={handleRestoreFiles}
				loading={restoringFiles}
				disabled={restoringFiles || selectedPaths.length === 0}
			/>
		{/if}
	{/snippet}
</ResponsiveDialog>

<VolumeBackupPolicyDialog
	bind:open={showBackupPolicy}
	{volumeName}
	policy={backupPolicy}
	onSaved={(policy) => (backupPolicy = policy)}
/>
