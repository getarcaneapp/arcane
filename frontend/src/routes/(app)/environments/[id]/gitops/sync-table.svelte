<script lang="ts">
	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { LifecycleIndicator } from '#lib/components/lifecycle-indicator/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import RemoveMenuItem from '#lib/components/arcane-table/cells/remove-menu-item.svelte';
	import BackupStateBadge from '#lib/components/gitops/backup-state-badge.svelte';
	import BackupHistoryDialog from '#lib/components/gitops/backup-history-dialog.svelte';
	import BackupResolveDialog from '#lib/components/gitops/backup-resolve-dialog.svelte';
	import { toast } from 'svelte-sonner';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/try-catch.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import type { FilterMap, Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import type { GitOpsSync } from '#lib/types/automation.js';
	import type { ColumnSpec, BulkAction, ArcaneRow } from '#lib/components/arcane-table/index.js';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import { formatDateTimeShort } from '#lib/utils/formatting.js';
	import { m } from '#lib/paraglide/messages.js';
	import { gitOpsSyncService } from '#lib/services/gitops-sync-service.js';
	import { toGitRouteUrl } from '#lib/utils/navigation.js';
	import { cn } from '#lib/utils.js';
	import {
		EditIcon as PencilIcon,
		StartIcon as PlayIcon,
		TrashIcon as Trash2Icon,
		RefreshIcon as RefreshCwIcon,
		GitBranchIcon,
		ProjectsIcon as FolderIcon,
		HashIcon,
		UploadIcon,
		DownloadIcon,
		ClockIcon,
		SettingsIcon,
		AlertTriangleIcon
	} from '#lib/icons/index.js';
	import { bulkConfirmAndRun, confirmAndRun } from '#lib/utils/bulk-actions.js';

	type FieldVisibility = Record<string, boolean>;

	let {
		environmentId,
		syncs = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable(),
		onEditSync
	}: {
		environmentId: string;
		syncs: Paginated<GitOpsSync>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
		onEditSync: (sync: GitOpsSync) => void;
	} = $props();

	const canDelete = $derived(hasPermission('gitops:delete', environmentId));
	const canBackup = $derived(hasPermission('gitops:backup', environmentId));
	const canRunBackup = $derived(canBackup && hasPermission('gitops:sync', environmentId));
	const canEditBackup = $derived(canBackup && hasPermission('gitops:update', environmentId));

	let isLoading = $state({
		removing: false,
		syncing: false
	});
	let mobileFieldVisibility = $state<Record<string, boolean>>({});
	let backupHistoryOpen = $state(false);
	let backupResolveOpen = $state(false);
	let backupDialogSync = $state<GitOpsSync | null>(null);

	const modeOptions = [
		{ value: '', label: m.common_all() },
		{ value: 'deploy', label: m.deployments() },
		{ value: 'backup', label: m.backups() }
	];

	const activeMode = $derived(String(requestOptions?.filters?.['mode'] ?? ''));

	async function reloadSyncs() {
		await handleApiResultWithCallbacks({
			result: await tryCatch(gitOpsSyncService.getSyncs(environmentId, requestOptions)),
			message: m.common_refresh_failed({ resource: m.git_syncs_title() }),
			setLoadingState: () => {},
			onSuccess: (next) => {
				syncs = next;
			}
		});
	}

	async function selectMode(mode: string) {
		const filters: FilterMap = { ...(requestOptions?.filters ?? {}) };
		if (mode) filters['mode'] = mode;
		else delete filters['mode'];

		requestOptions = {
			...requestOptions,
			filters: Object.keys(filters).length > 0 ? filters : undefined,
			pagination: { page: 1, limit: requestOptions?.pagination?.limit ?? 20 }
		};
		await reloadSyncs();
	}

	function getProjectDetailsUrl(projectId: string): string {
		const params = new URLSearchParams({
			from: 'gitops',
			environmentId
		});

		return `/projects/${projectId}?${params.toString()}`;
	}

	function isBackup(sync: GitOpsSync): boolean {
		return sync.mode === 'backup';
	}

	function openBackupHistory(sync: GitOpsSync) {
		backupDialogSync = sync;
		backupHistoryOpen = true;
	}

	function openBackupResolve(sync: GitOpsSync) {
		backupDialogSync = sync;
		backupResolveOpen = true;
	}

	async function handleDeleteSelected(ids: string[]) {
		bulkConfirmAndRun({
			ids,
			title: m.common_remove_title({ resource: `${ids.length} ${m.resource_sync()}(s)` }),
			message: m.common_remove_message({ resource: `${ids.length} ${m.resource_sync()}(s)` }),
			confirmLabel: m.common_remove(),
			destructive: true,
			run: (id) => gitOpsSyncService.deleteSync(environmentId, id),
			messages: {
				success: (count) => m.common_delete_success({ resource: `${count} ${m.resource_sync()}(s)` }),
				partial: (_success, _total, failed) => m.common_delete_failed({ resource: `${failed} items` }),
				failure: () => m.common_delete_failed({ resource: `${ids.length} items` })
			},
			setLoading: (loading) => (isLoading.removing = loading),
			onItemFailure: (id) => {
				const sync = syncs.data.find((item) => item.id === id);
				toast.error(m.common_delete_failed({ resource: sync?.name ?? m.common_unknown() }));
			},
			onComplete: async (result) => {
				if (result.success > 0) {
					syncs = await gitOpsSyncService.getSyncs(environmentId, requestOptions);
				}
			},
			clearSelection: () => (selectedIds = []),
			sequential: true
		});
	}

	async function handleDeleteOne(id: string, name: string) {
		const safeName = name ?? m.common_unknown();
		confirmAndRun({
			title: m.git_sync_remove_confirm(),
			message: m.git_sync_remove_message(),
			confirmLabel: m.common_remove(),
			destructive: true,
			setLoading: (loading) => (isLoading.removing = loading),
			run: () => gitOpsSyncService.deleteSync(environmentId, id),
			failureMessage: m.common_delete_failed({ resource: safeName }),
			onSuccess: async () => {
				toast.success(m.common_delete_success({ resource: `${m.resource_sync()} "${safeName}"` }));
				syncs = await gitOpsSyncService.getSyncs(environmentId, requestOptions);
			}
		});
	}

	async function handleDisconnectBackup(sync: GitOpsSync) {
		confirmAndRun({
			title: m.disconnect_backup_title({ name: sync.projectName || sync.name }),
			message: m.disconnect_backup_message(),
			confirmLabel: m.common_disconnect(),
			destructive: true,
			setLoading: (loading) => (isLoading.removing = loading),
			run: () => gitOpsSyncService.deleteSync(environmentId, sync.id),
			failureMessage: m.disconnect_backup_failed(),
			onSuccess: async () => {
				toast.success(m.disconnect_backup_success());
				await reloadSyncs();
			}
		});
	}

	async function handlePerformSync(sync: GitOpsSync) {
		isLoading.syncing = true;
		const result = await tryCatch(gitOpsSyncService.performSync(environmentId, sync.id));
		await handleApiResultWithCallbacks({
			result,
			message: isBackup(sync) ? m.backup_failed() : m.git_sync_failed(),
			setLoadingState: () => {},
			onSuccess: async () => {
				toast.success(isBackup(sync) ? m.backup_completed() : m.git_sync_success());
				await reloadSyncs();
			}
		});
		isLoading.syncing = false;
	}

	const columns = [
		{ accessorKey: 'id', title: m.common_id(), hidden: true },
		{
			accessorKey: 'name',
			title: m.git_sync_name(),
			sortable: true,
			cell: NameCell
		},
		{
			accessorKey: 'mode',
			title: m.direction(),
			sortable: true,
			cell: DirectionCell
		},
		{
			accessorKey: 'branch',
			title: m.git_sync_branch(),
			sortable: true,
			cell: BranchCell
		},
		{
			accessorKey: 'composePath',
			title: m.repository_path(),
			sortable: true,
			cell: PathCell
		},
		{
			accessorKey: 'autoSync',
			title: m.git_sync_auto_sync(),
			sortable: true,
			cell: AutoSyncCell
		},
		{
			accessorKey: 'lastSyncStatus',
			title: m.git_sync_status(),
			sortable: true,
			cell: StatusCell
		},
		{
			accessorKey: 'lastSyncCommit',
			title: m.commit(),
			sortable: true,
			cell: CommitCell
		},
		{
			accessorKey: 'lastSyncAt',
			title: m.git_sync_last_sync(),
			sortable: true,
			cell: LastSyncCell
		}
	] satisfies ColumnSpec<GitOpsSync>[];

	const mobileFields = [
		{ id: 'id', label: m.common_id(), defaultVisible: false },
		{ id: 'name', label: m.git_sync_name(), defaultVisible: true },
		{ id: 'mode', label: m.direction(), defaultVisible: true },
		{ id: 'branch', label: m.git_sync_branch(), defaultVisible: true },
		{ id: 'composePath', label: m.repository_path(), defaultVisible: true },
		{ id: 'autoSync', label: m.git_sync_auto_sync(), defaultVisible: true },
		{ id: 'lastSyncStatus', label: m.git_sync_status(), defaultVisible: true },
		{ id: 'backupState', label: m.backup_state(), defaultVisible: true },
		{ id: 'lastSyncCommit', label: m.commit(), defaultVisible: false },
		{ id: 'lastSyncAt', label: m.git_sync_last_sync(), defaultVisible: true }
	];

	const bulkActions = $derived.by<BulkAction[]>(() => [
		{
			id: 'remove',
			label: m.common_remove_selected_count({ count: selectedIds?.length ?? 0 }),
			action: 'remove',
			onClick: handleDeleteSelected,
			loading: isLoading.removing,
			disabled: isLoading.removing,
			icon: Trash2Icon
		}
	]);
</script>

{#snippet NameCell({ item, value }: { item: GitOpsSync; value: any; row: ArcaneRow<GitOpsSync> })}
	<span class="inline-flex items-center gap-1.5">
		{#if item.projectId}
			<a class="font-medium hover:underline" href={getProjectDetailsUrl(item.projectId)}>
				{value}
			</a>
		{:else}
			<span class="font-medium">{value}</span>
		{/if}
		<LifecycleIndicator scriptPath={item.preDeployScriptPath} />
	</span>
{/snippet}

{#snippet DirectionCell({ item }: { value: any; item: GitOpsSync; row: ArcaneRow<GitOpsSync> })}
	<span class="inline-flex items-center gap-1.5 text-sm">
		{#if isBackup(item)}
			<UploadIcon class="size-3.5 text-muted-foreground" />
			{m.push()}
		{:else}
			<DownloadIcon class="size-3.5 text-muted-foreground" />
			{m.pull()}
		{/if}
	</span>
{/snippet}

{#snippet BranchCell({ value }: { value: any; item: GitOpsSync; row: ArcaneRow<GitOpsSync> })}
	<div class="flex items-center gap-1.5">
		<GitBranchIcon class="size-3.5 text-muted-foreground" />
		<code class="rounded bg-muted px-2 py-0.5 text-xs text-muted-foreground">{value}</code>
	</div>
{/snippet}

{#snippet PathCell({ value, item }: { value: any; item: GitOpsSync; row: ArcaneRow<GitOpsSync> })}
	{@const path = isBackup(item) ? (item.backupDirectory ?? '') : String(value ?? '')}
	{@const route = isBackup(item) ? 'tree' : 'blob'}
	{@const fileUrl = item.repository?.url && path ? toGitRouteUrl(item.repository.url, route, item.branch, path) : null}
	<div class="flex items-center gap-1.5">
		<FolderIcon class="size-3.5 text-muted-foreground" />
		{#if !path}
			<span class="text-sm text-muted-foreground">{m.common_na()}</span>
		{:else if fileUrl}
			<a
				href={fileUrl}
				target="_blank"
				rel="noopener noreferrer"
				class="rounded bg-muted px-2 py-0.5 font-mono text-xs text-muted-foreground transition-colors hover:text-primary"
			>
				{path}
			</a>
		{:else}
			<code class="rounded bg-muted px-2 py-0.5 text-xs text-muted-foreground">{path}</code>
		{/if}
	</div>
{/snippet}

{#snippet AutoSyncCell({ value }: { value: any; item: GitOpsSync; row: ArcaneRow<GitOpsSync> })}
	<Badge variant={value ? 'blue' : 'gray'} minWidth="20">{value ? m.common_enabled() : m.common_disabled()}</Badge>
{/snippet}

{#snippet StatusCell({ value, item }: { value: any; item: GitOpsSync; row: ArcaneRow<GitOpsSync> })}
	{#if isBackup(item)}
		<BackupStateBadge sync={item} />
	{:else if value === 'success'}
		<Badge variant="green" minWidth="20">{m.common_success()}</Badge>
	{:else if value === 'failed'}
		<Badge variant="red" minWidth="20">{m.common_failed()}</Badge>
	{:else if value === 'pending'}
		<Badge variant="amber" minWidth="20">{m.common_pending()}</Badge>
	{:else}
		<Badge variant="gray" minWidth="20">{m.common_na()}</Badge>
	{/if}
{/snippet}

{#snippet CommitCell({ value, item }: { value: any; item: GitOpsSync; row: ArcaneRow<GitOpsSync> })}
	{#if value}
		{@const commitUrl = item.repository?.url ? toGitRouteUrl(item.repository.url, 'commit', String(value)) : null}
		<div class="flex items-center gap-1.5">
			<HashIcon class="size-3.5 text-muted-foreground" />
			{#if commitUrl}
				<a
					href={commitUrl}
					target="_blank"
					class="rounded bg-muted px-2 py-0.5 font-mono text-xs text-muted-foreground transition-colors hover:text-primary"
				>
					{value}
				</a>
			{:else}
				<code class="rounded bg-muted px-2 py-0.5 font-mono text-xs text-muted-foreground">
					{value}
				</code>
			{/if}
		</div>
	{:else}
		<span class="text-sm text-muted-foreground">{m.common_na()}</span>
	{/if}
{/snippet}

{#snippet LastSyncCell({ value, item }: { value: any; item: GitOpsSync; row: ArcaneRow<GitOpsSync> })}
	{@const timestamp = isBackup(item) ? item.lastBackupAt : value}
	<span class="text-sm">{timestamp ? formatDateTimeShort(timestamp) : m.common_never()}</span>
{/snippet}

{#snippet ModeFilter()}
	<div
		class="inline-flex items-center gap-0.5 rounded-lg border border-border/60 bg-muted/40 p-0.5"
		role="group"
		aria-label={m.direction()}
	>
		{#each modeOptions as option (option.value)}
			<button
				type="button"
				aria-pressed={activeMode === option.value}
				class={cn(
					'inline-flex h-7 items-center justify-center rounded-md px-2.5 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none',
					activeMode === option.value && 'bg-primary/15 text-primary ring-1 ring-primary/30'
				)}
				onclick={() => selectMode(option.value)}
			>
				{option.label}
			</button>
		{/each}
	</div>
{/snippet}

{#snippet BackupStateField(value: GitOpsSync)}
	<BackupStateBadge sync={value} />
{/snippet}

{#snippet SyncMobileCardSnippet({ item, mobileFieldVisibility }: { item: GitOpsSync; mobileFieldVisibility: FieldVisibility })}
	<UniversalMobileCard
		{item}
		icon={{ component: RefreshCwIcon, variant: 'purple' as const }}
		title={(item) => item.name}
		subtitle={(item) => ((mobileFieldVisibility['id'] ?? false) ? item.id : item.branch)}
		badges={[{ variant: 'purple' as const, text: m.resource_sync_cap() }]}
		fields={[
			{
				label: m.direction(),
				getValue: (item: GitOpsSync) => (isBackup(item) ? m.push() : m.pull()),
				icon: isBackup(item) ? UploadIcon : DownloadIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['mode'] ?? true
			},
			{
				label: m.git_sync_branch(),
				getValue: (item: GitOpsSync) => item.branch,
				icon: GitBranchIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['branch'] ?? true
			},
			{
				label: m.repository_path(),
				getValue: (item: GitOpsSync) => (isBackup(item) ? (item.backupDirectory ?? m.common_na()) : item.composePath),
				icon: FolderIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['composePath'] ?? true
			},
			{
				label: m.backup_state(),
				getValue: (item: GitOpsSync) => item,
				type: 'component' as const,
				component: BackupStateField,
				icon: UploadIcon,
				iconVariant: 'gray' as const,
				show: isBackup(item) && (mobileFieldVisibility['backupState'] ?? true)
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: GitOpsSync })}
	<RowActionsMenu>
		{#if isBackup(item)}
			<DropdownMenu.Item onclick={() => handlePerformSync(item)} disabled={isLoading.syncing || !canRunBackup}>
				<UploadIcon class="size-4" />
				{m.back_up_now()}
			</DropdownMenu.Item>

			<DropdownMenu.Item onclick={() => openBackupHistory(item)} disabled={!canBackup}>
				<ClockIcon class="size-4" />
				{m.history()}
			</DropdownMenu.Item>

			{#if item.backupState === 'needs_attention' && canBackup}
				<DropdownMenu.Item onclick={() => openBackupResolve(item)}>
					<AlertTriangleIcon class="size-4" />
					{m.resolve()}
				</DropdownMenu.Item>
			{/if}

			<DropdownMenu.Item onclick={() => onEditSync(item)} disabled={!canEditBackup}>
				<SettingsIcon class="size-4" />
				{m.settings()}
			</DropdownMenu.Item>

			{#if canDelete && canBackup}
				<RemoveMenuItem
					onclick={() => handleDisconnectBackup(item)}
					disabled={isLoading.removing}
					label={m.common_disconnect()}
				/>
			{/if}
		{:else}
			<DropdownMenu.Item onclick={() => handlePerformSync(item)} disabled={isLoading.syncing}>
				<PlayIcon class="size-4" />
				{m.pull_from_git()}
			</DropdownMenu.Item>

			<DropdownMenu.Item onclick={() => onEditSync(item)}>
				<PencilIcon class="size-4" />
				{m.common_edit()}
			</DropdownMenu.Item>

			{#if canDelete}
				<RemoveMenuItem onclick={() => handleDeleteOne(item.id, item.name)} disabled={isLoading.removing} />
			{/if}
		{/if}
	</RowActionsMenu>
{/snippet}

<ArcaneTable
	persistKey="arcane-gitops-syncs-table"
	items={syncs}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	{bulkActions}
	onRefresh={async (options) => (syncs = await gitOpsSyncService.getSyncs(environmentId, options))}
	{columns}
	{mobileFields}
	customToolbarActions={ModeFilter}
	rowActions={RowActions}
	mobileCard={SyncMobileCardSnippet}
/>

<BackupHistoryDialog bind:open={backupHistoryOpen} {environmentId} sync={backupDialogSync} />

<BackupResolveDialog bind:open={backupResolveOpen} {environmentId} sync={backupDialogSync} onResolved={reloadSyncs} />
