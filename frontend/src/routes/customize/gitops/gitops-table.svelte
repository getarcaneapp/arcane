<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import EllipsisIcon from '@lucide/svelte/icons/ellipsis';
	import PencilIcon from '@lucide/svelte/icons/pencil';
	import TestTubeIcon from '@lucide/svelte/icons/test-tube';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { toast } from 'svelte-sonner';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import type { GitOpsRepository } from '$lib/types/gitops-repository.type';
	import type { ColumnSpec } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table/index.js';
	import GitBranchIcon from '@lucide/svelte/icons/git-branch';
	import LinkIcon from '@lucide/svelte/icons/link';
	import { format } from 'date-fns';
	import { m } from '$lib/paraglide/messages';
	import { gitopsRepositoryService } from '$lib/services/gitops-repository-service';

	let {
		repositories = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable(),
		onEditRepository
	}: {
		repositories: Paginated<GitOpsRepository>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
		onEditRepository: (repository: GitOpsRepository) => void;
	} = $props();

	let isLoading = $state({
		removing: false,
		testing: false,
		syncing: false
	});

	async function handleDeleteSelected(ids: string[]) {
		if (!ids?.length) return;

		openConfirmDialog({
			title: `Remove ${ids.length} GitOps ${ids.length === 1 ? 'Repository' : 'Repositories'}?`,
			message: `Are you sure you want to remove ${ids.length} selected ${ids.length === 1 ? 'repository' : 'repositories'}?`,
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;

					let successCount = 0;
					let failureCount = 0;
					for (const id of ids) {
						const repo = repositories.data.find((r) => r.id === id);
						const result = await tryCatch(gitopsRepositoryService.deleteRepository(id));
						if (result.error) {
							failureCount++;
							toast.error(`Failed to delete repository: ${repo?.url ?? 'Unknown'}`);
						} else {
							successCount++;
						}
					}

					if (successCount > 0) {
						toast.success(`Successfully removed ${successCount} ${successCount === 1 ? 'repository' : 'repositories'}`);
						repositories = await gitopsRepositoryService.getRepositories(requestOptions);
					}
					if (failureCount > 0) toast.error(`Failed to remove ${failureCount} ${failureCount === 1 ? 'repository' : 'repositories'}`);

					selectedIds = [];
					isLoading.removing = false;
				}
			}
		});
	}

	async function handleDeleteOne(id: string, url: string) {
		const safeUrl = url ?? m.common_unknown();
		openConfirmDialog({
			title: 'Remove GitOps Repository?',
			message: `Are you sure you want to remove "${safeUrl}"?`,
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;

					const result = await tryCatch(gitopsRepositoryService.deleteRepository(id));
					handleApiResultWithCallbacks({
						result,
						message: `Failed to delete repository: ${safeUrl}`,
						setLoadingState: () => {},
						onSuccess: async () => {
							toast.success(`Successfully deleted repository "${safeUrl}"`);
							repositories = await gitopsRepositoryService.getRepositories(requestOptions);
						}
					});

					isLoading.removing = false;
				}
			}
		});
	}

	async function handleTest(id: string, url: string) {
		isLoading.testing = true;
		const safeUrl = url ?? m.common_unknown();
		const result = await tryCatch(gitopsRepositoryService.testRepository(id));
		handleApiResultWithCallbacks({
			result,
			message: `Connection test failed for ${safeUrl}`,
			setLoadingState: () => {},
			onSuccess: (resp) => {
				const msg = typeof resp === 'object' && resp !== null && 'message' in resp 
					? String(resp.message) 
					: m.common_unknown();
				toast.success(`${safeUrl}: ${msg}`);
			}
		});
		isLoading.testing = false;
	}

	async function handleSyncNow(id: string, url: string) {
		isLoading.syncing = true;
		const safeUrl = url ?? m.common_unknown();
		const result = await tryCatch(gitopsRepositoryService.syncRepositoryNow(id));
		handleApiResultWithCallbacks({
			result,
			message: `Sync failed for ${safeUrl}`,
			setLoadingState: () => {},
			onSuccess: (resp) => {
				const msg = typeof resp === 'object' && resp !== null && 'message' in resp 
					? String(resp.message) 
					: 'Synced successfully';
				toast.success(`${safeUrl}: ${msg}`);
				// Refresh the list
				gitopsRepositoryService.getRepositories(requestOptions).then((newRepos) => {
					repositories = newRepos;
				});
			}
		});
		isLoading.syncing = false;
	}

	const columns = [
		{ accessorKey: 'id', title: m.common_id(), hidden: true },
		{
			accessorKey: 'url',
			title: 'Repository URL',
			sortable: true,
			cell: UrlCell
		},
		{
			accessorKey: 'branch',
			title: 'Branch',
			sortable: true,
			cell: BranchCell
		},
		{
			accessorKey: 'composePath',
			title: 'Compose Path',
			sortable: true
		},
		{
			accessorKey: 'autoSync',
			title: 'Auto Sync',
			sortable: true,
			cell: AutoSyncCell
		},
		{
			accessorKey: 'enabled',
			title: m.common_enabled(),
			sortable: true,
			cell: EnabledCell
		},
		{
			accessorKey: 'lastSyncedAt',
			title: 'Last Synced',
			sortable: true,
			cell: LastSyncedCell
		},
		{
			accessorKey: 'createdAt',
			title: m.common_created_at(),
			sortable: true,
			cell: CreatedAtCell
		},
		{
			accessorKey: 'actions',
			title: '',
			sortable: false,
			cell: ActionsCell
		}
	] satisfies ColumnSpec<GitOpsRepository>[];
</script>

{#snippet UrlCell(row: GitOpsRepository)}
	<div class="flex items-center gap-2">
		<LinkIcon class="size-4 text-muted-foreground" />
		<span class="font-mono text-sm">{row.url}</span>
	</div>
{/snippet}

{#snippet BranchCell(row: GitOpsRepository)}
	<div class="flex items-center gap-2">
		<GitBranchIcon class="size-4 text-muted-foreground" />
		<span class="font-mono text-sm">{row.branch}</span>
	</div>
{/snippet}

{#snippet AutoSyncCell(row: GitOpsRepository)}
	<StatusBadge status={row.autoSync ? 'running' : 'stopped'}>
		{row.autoSync ? 'Enabled' : 'Disabled'}
	</StatusBadge>
{/snippet}

{#snippet EnabledCell(row: GitOpsRepository)}
	<StatusBadge status={row.enabled ? 'running' : 'stopped'}>
		{row.enabled ? m.common_enabled() : m.common_disabled()}
	</StatusBadge>
{/snippet}

{#snippet LastSyncedCell(row: GitOpsRepository)}
	{#if row.lastSyncedAt}
		<span class="text-sm">{format(new Date(row.lastSyncedAt), 'MMM d, yyyy HH:mm')}</span>
	{:else}
		<span class="text-muted-foreground text-sm">Never</span>
	{/if}
{/snippet}

{#snippet CreatedAtCell(row: GitOpsRepository)}
	<span class="text-sm">{format(new Date(row.createdAt), 'MMM d, yyyy HH:mm')}</span>
{/snippet}

{#snippet ActionsCell(row: GitOpsRepository)}
	<div class="flex items-center justify-end gap-2">
		<DropdownMenu.Root>
			<DropdownMenu.Trigger asChild let:builder>
				<Button builders={[builder]} variant="ghost" size="icon" class="size-8">
					<EllipsisIcon class="size-4" />
				</Button>
			</DropdownMenu.Trigger>
			<DropdownMenu.Content align="end">
				<DropdownMenu.Item onclick={() => onEditRepository(row)}>
					<PencilIcon class="mr-2 size-4" />
					{m.common_edit()}
				</DropdownMenu.Item>
				<DropdownMenu.Item onclick={() => handleTest(row.id, row.url)}>
					<TestTubeIcon class="mr-2 size-4" />
					Test Connection
				</DropdownMenu.Item>
				<DropdownMenu.Item onclick={() => handleSyncNow(row.id, row.url)}>
					<RefreshCwIcon class="mr-2 size-4" />
					Sync Now
				</DropdownMenu.Item>
				<DropdownMenu.Separator />
				<DropdownMenu.Item class="text-destructive" onclick={() => handleDeleteOne(row.id, row.url)}>
					<Trash2Icon class="mr-2 size-4" />
					{m.common_remove()}
				</DropdownMenu.Item>
			</DropdownMenu.Content>
		</DropdownMenu.Root>
	</div>
{/snippet}

{#snippet MobileCard(row: GitOpsRepository)}
	<UniversalMobileCard
		id={row.id}
		title={row.url}
		subtitle={`Branch: ${row.branch}`}
		icon={GitBranchIcon}
		status={row.enabled ? 'running' : 'stopped'}
		statusText={row.enabled ? m.common_enabled() : m.common_disabled()}
		bind:selectedIds
		onEdit={() => onEditRepository(row)}
		onDelete={() => handleDeleteOne(row.id, row.url)}
		{...{
			sections: [
				{ label: 'Compose Path', value: row.composePath },
				{ label: 'Auto Sync', value: row.autoSync ? 'Enabled' : 'Disabled' },
				{ label: 'Last Synced', value: row.lastSyncedAt ? format(new Date(row.lastSyncedAt), 'MMM d, yyyy HH:mm') : 'Never' }
			],
			actions: [
				{ label: 'Test Connection', icon: TestTubeIcon, onClick: () => handleTest(row.id, row.url) },
				{ label: 'Sync Now', icon: RefreshCwIcon, onClick: () => handleSyncNow(row.id, row.url) }
			]
		}}
	/>
{/snippet}

<ArcaneTable
	bind:data={repositories}
	bind:selectedIds
	bind:requestOptions
	{columns}
	enableSelection={true}
	enableFilters={true}
	enableSearch={true}
	mobileCardSnippet={MobileCard}
	onDeleteSelected={handleDeleteSelected}
/>
