<script lang="ts">
	import { m } from '$lib/paraglide/messages';
	import { volumeBackupService, type VolumeBackupListResponse } from '$lib/services/volume-backup-service';
	import { volumeService } from '$lib/services/volume-service';
	import type { BackupEntry } from '$lib/types/shared';
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
		AlertIcon
	} from '$lib/icons';
	import { ArcaneButton } from '$lib/components/arcane-button';
	import { toast } from 'svelte-sonner';
	import { bytes } from '$lib/utils/formatting';
	import { format } from 'date-fns';
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

	let requestOptions = $state<SearchPaginationSortRequest>({
		pagination: { page: 1, limit: 10 },
		sort: { column: 'createdAt', direction: 'desc' }
	});

	let creating = $state(false);
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
		} catch (e: any) {
			toast.error(e.message || 'Failed to load backups');
			return backupsPaginated;
		}
	}

	async function handleCreate() {
		creating = true;
		try {
			const result = await volumeBackupService.createBackup(volumeName);
			toast.success(m.common_success(), activityToastOptions(extractActivityId(result)));
			await loadData(requestOptions);
		} catch (e: any) {
			toast.error(e.message || m.common_failed());
		} finally {
			creating = false;
		}
	}

	async function handleDelete(backup: BackupEntry) {
		openConfirmDialog({
			title: m.common_remove_title({ resource: 'Backup' }),
			message: m.volumes_backup_delete_confirm(),
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
					try {
						const result = await volumeBackupService.deleteBackup(backup.id);
						toast.success(m.common_delete_success({ resource: 'Backup' }), activityToastOptions(extractActivityId(result)));
						await loadData(requestOptions);
					} catch (e: any) {
						toast.error(e.message || m.common_delete_failed({ resource: 'Backup' }));
					}
				}
			}
		});
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
		} catch (e: any) {
			toast.error(e.message || m.common_failed());
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
					} catch (e: any) {
						toast.error(e.message || m.common_failed());
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
		} catch (e: any) {
			toast.error(e.message || m.common_failed());
		} finally {
			restoringFiles = false;
		}
	}

	function formatBytes(value: number): string {
		return bytes.format(value, { unitSeparator: ' ' }) ?? '-';
	}

	onMount(async () => {
		await loadData(requestOptions);
	});

	const columns = [
		{ accessorKey: 'id', title: m.common_id(), sortable: true, cell: IdCell },
		{ accessorKey: 'size', title: m.common_size(), sortable: true, cell: SizeCell },
		{ accessorKey: 'createdAt', title: m.common_created(), sortable: true, cell: CreatedCell }
	] satisfies ColumnSpec<BackupEntry>[];

	const mobileFields = [{ id: 'size', label: m.common_size(), defaultVisible: true }];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet IdCell({ value }: { value: any })}
	<code class="font-mono text-xs font-medium">{value}</code>
{/snippet}

{#snippet SizeCell({ value }: { value: any })}
	{formatBytes(Number(value))}
{/snippet}

{#snippet CreatedCell({ value }: { value: any })}
	{format(new Date(String(value)), 'PP p')}
{/snippet}

{#snippet RowActions({ item }: { item: BackupEntry })}
	<RowActionsMenu openMenuLabel={m.common_open_menu()}>
		{#if canBackupVolume}
			<DropdownMenu.Item onclick={() => handleRestore(item)}>
				<RestartIcon class="size-4" />
				Restore
			</DropdownMenu.Item>
			<DropdownMenu.Item onclick={() => openRestoreFilesDialog(item)}>
				<FileTextIcon class="size-4" />
				Restore files
			</DropdownMenu.Item>
		{/if}
		<DropdownMenu.Item onclick={() => volumeBackupService.downloadBackup(item.id)}>
			<DownloadIcon class="size-4" />
			{m.templates_download()}
		</DropdownMenu.Item>
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
			action="create"
			customLabel={m.volumes_backup_create()}
			loading={creating}
			disabled={creating}
			onclick={handleCreate}
			size="sm"
			icon={AddIcon}
		/>
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
				label: m.common_size(),
				getValue: (item) => formatBytes(item.size),
				icon: InfoIcon,
				iconVariant: 'gray',
				show: mobileFieldVisibility['size'] ?? true
			}
		]}
		footer={{
			label: m.common_created(),
			getValue: (item) => format(new Date(item.createdAt), 'PP p'),
			icon: ClockIcon
		}}
		rowActions={RowActions}
	/>
{/snippet}

<div class="space-y-4">
	<div class="flex items-center justify-between">
		<h2 class="text-lg font-semibold">{m.volumes_backups_title()}</h2>
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
	description="Select files from this backup to restore."
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
						<LoadingSpinnerIcon class="text-muted-foreground size-5" />
					</div>
				{:else if filteredBackupFiles.length === 0}
					<div class="text-muted-foreground flex items-center justify-center py-8 text-sm">{m.volume_backup_no_files()}</div>
				{:else}
					<div class="divide-border/40 divide-y">
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
