<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { toast } from 'svelte-sonner';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import type { GitOpsSync } from '$lib/types/gitops.type';
	import type { Row } from '@tanstack/table-core';
	import type { ColumnSpec } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table/index.js';
	import { format } from 'date-fns';
	import { m } from '$lib/paraglide/messages';
	import { gitOpsSyncService } from '$lib/services/gitops-sync-service';
	import {
		EllipsisIcon,
		EditIcon as PencilIcon,
		StartIcon as PlayIcon,
		TrashIcon as Trash2Icon,
		RefreshIcon as RefreshCwIcon,
		GitBranchIcon,
		ProjectsIcon as FolderIcon,
		HashIcon
	} from '$lib/icons';
	import { goto } from '$app/navigation';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';

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

	async function handleDeleteSelected(ids: string[]) {
		if (!ids?.length) return;

		openConfirmDialog({
			title: m.common_remove_title({ resource: `${ids.length} ${m.resource_sync()}(s)` }),
			message: m.common_remove_message({ resource: `${ids.length} ${m.resource_sync()}(s)` }),
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;

					let successCount = 0;
					let failureCount = 0;
					for (const id of ids) {
						const sync = syncs.data.find((s) => s.id === id);
						const result = await tryCatch(gitOpsSyncService.deleteSync(environmentId, id));
						if (result.error) {
							failureCount++;
							toast.error(m.common_delete_failed({ resource: sync?.name ?? m.common_unknown() }));
						} else {
							successCount++;
						}
					}

					if (successCount > 0) {
						toast.success(m.common_delete_success({ resource: `${successCount} ${m.resource_sync()}(s)` }));
						syncs = await gitOpsSyncService.getSyncs(environmentId, requestOptions);
					}
					if (failureCount > 0) toast.error(m.common_delete_failed({ resource: `${failureCount} items` }));

					selectedIds = [];
					isLoading.removing = false;
				}
			}
		});
	}

	async function handleDeleteOne(id: string, name: string) {
		const safeName = name ?? m.common_unknown();
		openConfirmDialog({
			title: m.git_sync_remove_confirm(),
			message: m.git_sync_remove_message(),
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;

					const result = await tryCatch(gitOpsSyncService.deleteSync(environmentId, id));
					handleApiResultWithCallbacks({
						result,
						message: m.common_delete_failed({ resource: safeName }),
						setLoadingState: () => {},
						onSuccess: async () => {
							toast.success(m.common_delete_success({ resource: `${m.resource_sync()} "${safeName}"` }));
							syncs = await gitOpsSyncService.getSyncs(environmentId, requestOptions);
						}
					});

					isLoading.removing = false;
				}
			}
		});
	}

	async function handlePerformSync(id: string, name: string) {
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
			title: 'Commit',
			sortable: true,
			cell: CommitCell
		},
		{
			accessorKey: 'lastSyncAt',
			title: m.git_sync_last_sync(),
			sortable: true,
			cell: LastSyncCell
		},
		{
			accessorKey: 'enabled',
			title: m.common_status(),
			sortable: true,
			cell: EnabledCell
		}
	] satisfies ColumnSpec<GitOpsSync>[];

	const mobileFields = [
		{ id: 'id', label: m.common_id(), defaultVisible: false },
		{ id: 'name', label: m.git_sync_name(), defaultVisible: true },
		{ id: 'branch', label: m.git_sync_branch(), defaultVisible: true },
		{ id: 'composePath', label: m.git_sync_compose_path(), defaultVisible: true },
		{ id: 'autoSync', label: m.git_sync_auto_sync(), defaultVisible: true },
		{ id: 'lastSyncStatus', label: m.git_sync_status(), defaultVisible: true },
		{ id: 'lastSyncCommit', label: 'Commit', defaultVisible: false },
		{ id: 'lastSyncAt', label: m.git_sync_last_sync(), defaultVisible: true },
		{ id: 'enabled', label: m.common_status(), defaultVisible: true }
	];
</script>

{#snippet NameCell({ item, value }: { item: GitOpsSync; value: any; row: Row<GitOpsSync> })}
	<a class="font-medium hover:underline" href="/projects/{item.projectId}">
		{value}
	</a>
{/snippet}

{#snippet BranchCell({ value }: { value: any; item: GitOpsSync; row: Row<GitOpsSync> })}
	<div class="flex items-center gap-1.5">
		<GitBranchIcon class="text-muted-foreground size-3.5" />
		<code class="bg-muted text-muted-foreground rounded px-2 py-0.5 text-xs">{value}</code>
	</div>
{/snippet}

{#snippet PathCell({ value }: { value: any; item: GitOpsSync; row: Row<GitOpsSync> })}
	<div class="flex items-center gap-1.5">
		<FolderIcon class="text-muted-foreground size-3.5" />
		<code class="bg-muted text-muted-foreground rounded px-2 py-0.5 text-xs">{value}</code>
	</div>
{/snippet}

{#snippet AutoSyncCell({ value }: { value: any; item: GitOpsSync; row: Row<GitOpsSync> })}
	<StatusBadge variant={value ? 'blue' : 'gray'} text={value ? m.common_enabled() : m.common_disabled()} />
{/snippet}

{#snippet StatusCell({ value }: { value: any; item: GitOpsSync; row: Row<GitOpsSync> })}
	{#if value === 'success'}
		<StatusBadge variant="green" text={m.common_success()} />
	{:else if value === 'failed'}
		<StatusBadge variant="red" text={m.common_failed()} />
	{:else if value === 'pending'}
		<StatusBadge variant="amber" text={m.common_pending()} />
	{:else}
		<StatusBadge variant="gray" text={m.common_na()} />
	{/if}
{/snippet}

{#snippet CommitCell({ value, item }: { value: any; item: GitOpsSync; row: Row<GitOpsSync> })}
	{#if value}
		<div class="flex items-center gap-1.5">
			<HashIcon class="text-muted-foreground size-3.5" />
			{#if item.repository?.url}
				<a
					href="{item.repository.url.replace(/\.git$/, '')}/commit/{value}"
					target="_blank"
					class="hover:text-primary bg-muted text-muted-foreground rounded px-2 py-0.5 font-mono text-xs transition-colors"
				>
					{value}
				</a>
			{:else}
				<code class="bg-muted text-muted-foreground rounded px-2 py-0.5 font-mono text-xs">
					{value}
				</code>
			{/if}
		</div>
	{:else}
		<span class="text-muted-foreground text-sm">{m.common_na()}</span>
	{/if}
{/snippet}

{#snippet LastSyncCell({ value }: { value: any; item: GitOpsSync; row: Row<GitOpsSync> })}
	<span class="text-sm">{value ? format(new Date(value), 'PP p') : m.common_never()}</span>
{/snippet}

{#snippet EnabledCell({ value }: { value: any; item: GitOpsSync; row: Row<GitOpsSync> })}
	<StatusBadge variant={value ? 'green' : 'red'} text={value ? m.common_enabled() : m.common_disabled()} />
{/snippet}

{#snippet SyncMobileCardSnippet({
	item,
	mobileFieldVisibility,
	row
}: {
	item: GitOpsSync;
	mobileFieldVisibility: FieldVisibility;
	row: Row<GitOpsSync>;
})}
	<UniversalMobileCard
		{item}
		icon={{ component: RefreshCwIcon, variant: 'purple' as const }}
		title={(item) => item.name}
		subtitle={(item) => ((mobileFieldVisibility.id ?? false) ? item.id : item.branch)}
		badges={[{ variant: 'purple' as const, text: m.resource_sync_cap() }]}
		fields={[
			{
				label: m.git_sync_branch(),
				getValue: (item: GitOpsSync) => item.branch,
				icon: GitBranchIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.branch ?? true
			},
			{
				label: m.git_sync_compose_path(),
				getValue: (item: GitOpsSync) => item.composePath,
				icon: FolderIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.composePath ?? true
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item, row }: { item: GitOpsSync; row?: Row<GitOpsSync> })}
	<DropdownMenu.Root>
		<DropdownMenu.Trigger>
			{#snippet child({ props })}
				<Button {...props} variant="ghost" size="icon" class="relative size-8 p-0">
					<span class="sr-only">{m.common_open_menu()}</span>
					<EllipsisIcon />
				</Button>
			{/snippet}
		</DropdownMenu.Trigger>
		<DropdownMenu.Content align="end">
			<DropdownMenu.Group>
				<DropdownMenu.Item onclick={() => handlePerformSync(item.id, item.name)} disabled={isLoading.syncing}>
					<PlayIcon class="size-4" />
					{m.git_sync_perform()}
				</DropdownMenu.Item>
				<DropdownMenu.Item onclick={() => onEditSync(item)}>
					<PencilIcon class="size-4" />
					{m.common_edit()}
				</DropdownMenu.Item>
				<DropdownMenu.Item
					variant="destructive"
					onclick={() => handleDeleteOne(item.id, item.name)}
					disabled={isLoading.removing}
				>
					<Trash2Icon class="size-4" />
					{m.common_remove()}
				</DropdownMenu.Item>
			</DropdownMenu.Group>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/snippet}

<ArcaneTable
	persistKey="arcane-gitops-syncs-table"
	items={syncs}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	onRemoveSelected={(ids) => handleDeleteSelected(ids)}
	onRefresh={async (options) => (syncs = await gitOpsSyncService.getSyncs(environmentId, options))}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={SyncMobileCardSnippet}
/>
