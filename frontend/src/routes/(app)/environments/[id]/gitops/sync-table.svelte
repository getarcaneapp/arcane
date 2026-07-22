<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { Badge } from '$lib/components/ui/badge';
	import { LifecycleIndicator } from '$lib/components/lifecycle-indicator';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import RowActionsMenu from '$lib/components/arcane-table/row-actions-menu.svelte';
	import RemoveMenuItem from '$lib/components/arcane-table/cells/remove-menu-item.svelte';
	import { toast } from 'svelte-sonner';
	import { handleApiResultWithCallbacks } from '$lib/utils/api';
	import { tryCatch } from '$lib/utils/api';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/shared';
	import type { GitOpsSync } from '$lib/types/automation';
	import type { ColumnSpec, BulkAction, ArcaneRow } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table/index.js';
	import { formatDateTimeShort } from '$lib/utils/formatting';
	import { m } from '$lib/paraglide/messages';
	import { gitOpsSyncService } from '$lib/services/gitops-sync-service';
	import { toGitCommitUrl } from '$lib/utils/navigation';
	import {
		EditIcon as PencilIcon,
		StartIcon as PlayIcon,
		TrashIcon as Trash2Icon,
		RefreshIcon as RefreshCwIcon,
		GitBranchIcon,
		ProjectsIcon as FolderIcon,
		HashIcon
	} from '$lib/icons';
	import { bulkConfirmAndRun, confirmAndRun } from '$lib/utils/bulk-actions';

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

	let isLoading = $state({
		removing: false,
		syncing: false
	});
	let mobileFieldVisibility = $state<Record<string, boolean>>({});

	function getProjectDetailsUrl(projectId: string): string {
		const params = new URLSearchParams({
			from: 'gitops',
			environmentId
		});

		return `/projects/${projectId}?${params.toString()}`;
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

	async function handlePerformSync(id: string, _name: string) {
		isLoading.syncing = true;
		const result = await tryCatch(gitOpsSyncService.performSync(environmentId, id));
		handleApiResultWithCallbacks({
			result,
			message: m.git_sync_failed(),
			setLoadingState: () => {},
			onSuccess: () => {
				toast.success(m.git_sync_success());
				gitOpsSyncService.getSyncs(environmentId, requestOptions).then((newSyncs) => {
					syncs = newSyncs;
				});
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
			accessorKey: 'branch',
			title: m.git_sync_branch(),
			sortable: true,
			cell: BranchCell
		},
		{
			accessorKey: 'composePath',
			title: m.git_sync_compose_path(),
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
		{ id: 'branch', label: m.git_sync_branch(), defaultVisible: true },
		{ id: 'composePath', label: m.git_sync_compose_path(), defaultVisible: true },
		{ id: 'autoSync', label: m.git_sync_auto_sync(), defaultVisible: true },
		{ id: 'lastSyncStatus', label: m.git_sync_status(), defaultVisible: true },
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

{#snippet BranchCell({ value }: { value: any; item: GitOpsSync; row: ArcaneRow<GitOpsSync> })}
	<div class="flex items-center gap-1.5">
		<GitBranchIcon class="size-3.5 text-muted-foreground" />
		<code class="rounded bg-muted px-2 py-0.5 text-xs text-muted-foreground">{value}</code>
	</div>
{/snippet}

{#snippet PathCell({ value }: { value: any; item: GitOpsSync; row: ArcaneRow<GitOpsSync> })}
	<div class="flex items-center gap-1.5">
		<FolderIcon class="size-3.5 text-muted-foreground" />
		<code class="rounded bg-muted px-2 py-0.5 text-xs text-muted-foreground">{value}</code>
	</div>
{/snippet}

{#snippet AutoSyncCell({ value }: { value: any; item: GitOpsSync; row: ArcaneRow<GitOpsSync> })}
	<Badge variant={value ? 'blue' : 'gray'} minWidth="20">{value ? m.common_enabled() : m.common_disabled()}</Badge>
{/snippet}

{#snippet StatusCell({ value }: { value: any; item: GitOpsSync; row: ArcaneRow<GitOpsSync> })}
	{#if value === 'success'}
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
		{@const commitUrl = item.repository?.url ? toGitCommitUrl(item.repository.url, String(value)) : null}
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

{#snippet LastSyncCell({ value }: { value: any; item: GitOpsSync; row: ArcaneRow<GitOpsSync> })}
	<span class="text-sm">{value ? formatDateTimeShort(value) : m.common_never()}</span>
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
				label: m.git_sync_branch(),
				getValue: (item: GitOpsSync) => item.branch,
				icon: GitBranchIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['branch'] ?? true
			},
			{
				label: m.git_sync_compose_path(),
				getValue: (item: GitOpsSync) => item.composePath,
				icon: FolderIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['composePath'] ?? true
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: GitOpsSync })}
	<RowActionsMenu>
		<DropdownMenu.Item onclick={() => handlePerformSync(item.id, item.name)} disabled={isLoading.syncing}>
			<PlayIcon class="size-4" />
			{m.git_sync_perform()}
		</DropdownMenu.Item>

		<DropdownMenu.Item onclick={() => onEditSync(item)}>
			<PencilIcon class="size-4" />
			{m.common_edit()}
		</DropdownMenu.Item>

		<RemoveMenuItem onclick={() => handleDeleteOne(item.id, item.name)} disabled={isLoading.removing} />
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
	rowActions={RowActions}
	mobileCard={SyncMobileCardSnippet}
/>
