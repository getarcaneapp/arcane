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
	import type { GitOpsRepository } from '$lib/types/gitops.type';
	import type { ColumnSpec } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table/index.js';
	import GitBranchIcon from '@lucide/svelte/icons/git-branch';
	import LinkIcon from '@lucide/svelte/icons/link';
	import { format } from 'date-fns';
	import { m } from '$lib/paraglide/messages';
	import { gitopsRepositoryService } from '$lib/services/gitops-service';

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
			title: m.gitops_remove_selected_title({ count: ids.length }),
			message: m.gitops_remove_selected_message({ count: ids.length }),
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
					if (failureCount > 0)
						toast.error(`Failed to remove ${failureCount} ${failureCount === 1 ? 'repository' : 'repositories'}`);

					selectedIds = [];
					isLoading.removing = false;
				}
			}
		});
	}

	async function handleDeleteOne(id: string, url: string) {
		const safeUrl = url ?? m.common_unknown();
		openConfirmDialog({
			title: m.gitops_remove_repository_title(),
			message: m.gitops_remove_repository_message({ url: safeUrl }),
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
			message: m.gitops_test_failed({ url: safeUrl }),
			setLoadingState: () => {},
			onSuccess: (resp) => {
				const msg = typeof resp === 'object' && resp !== null && 'message' in resp ? String(resp.message) : m.common_unknown();
				toast.success(m.gitops_test_success({ url: safeUrl, message: msg }));
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
			message: m.gitops_sync_failed({ url: safeUrl }),
			setLoadingState: () => {},
			onSuccess: (resp) => {
				const msg = typeof resp === 'object' && resp !== null && 'message' in resp ? String(resp.message) : 'Synced successfully';
				toast.success(m.gitops_sync_success({ url: safeUrl, message: msg }));
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
			title: m.gitops_repository_url(),
			sortable: true,
			cell: UrlCell
		},
		{
			accessorKey: 'branch',
			title: m.gitops_branch(),
			sortable: true,
			cell: BranchCell
		},
		{
			accessorKey: 'composePath',
			title: m.gitops_compose_path(),
			sortable: true
		},
		{
			accessorKey: 'autoSync',
			title: m.gitops_auto_sync(),
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
			title: m.gitops_last_synced(),
			sortable: true,
			cell: LastSyncedCell
		},
		{
			accessorKey: 'createdAt',
			title: m.common_created(),
			sortable: true,
			cell: CreatedAtCell
		}
	] satisfies ColumnSpec<GitOpsRepository>[];

	const mobileFields = [
		{ id: 'id', label: m.common_id(), defaultVisible: true },
		{ id: 'url', label: m.gitops_repository_url(), defaultVisible: true },
		{ id: 'branch', label: m.gitops_branch(), defaultVisible: true },
		{ id: 'composePath', label: m.gitops_compose_path(), defaultVisible: true },
		{ id: 'autoSync', label: m.gitops_auto_sync(), defaultVisible: true },
		{ id: 'lastSyncedAt', label: m.gitops_last_synced(), defaultVisible: true }
	];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet UrlCell({ item }: { item: GitOpsRepository })}
	<div class="flex items-center gap-2">
		<LinkIcon class="text-muted-foreground size-4" />
		<span class="font-mono text-sm">{item.url}</span>
	</div>
{/snippet}

{#snippet BranchCell({ item }: { item: GitOpsRepository })}
	<div class="flex items-center gap-2">
		<GitBranchIcon class="text-muted-foreground size-4" />
		<span class="font-mono text-sm">{item.branch}</span>
	</div>
{/snippet}

{#snippet AutoSyncCell({ item }: { item: GitOpsRepository })}
	<StatusBadge variant={item.autoSync ? 'green' : 'red'} text={item.autoSync ? 'Enabled' : 'Disabled'} />
{/snippet}

{#snippet EnabledCell({ item }: { item: GitOpsRepository })}
	<StatusBadge variant={item.enabled ? 'green' : 'red'} text={item.enabled ? m.common_enabled() : m.common_disabled()} />
{/snippet}

{#snippet LastSyncedCell({ item }: { item: GitOpsRepository })}
	{#if item.lastSyncedAt}
		<span class="text-sm">{format(new Date(item.lastSyncedAt), 'MMM d, yyyy HH:mm')}</span>
	{:else}
		<span class="text-muted-foreground text-sm">Never</span>
	{/if}
{/snippet}

{#snippet CreatedAtCell({ item }: { item: GitOpsRepository })}
	<span class="text-sm">{format(new Date(item.createdAt), 'MMM d, yyyy HH:mm')}</span>
{/snippet}

{#snippet RowActions({ item }: { item: GitOpsRepository })}
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
				<DropdownMenu.Item onclick={() => handleTest(item.id, item.url)} disabled={isLoading.testing}>
					<TestTubeIcon class="size-4" />
					{m.gitops_test_connection()}
				</DropdownMenu.Item>
				<DropdownMenu.Item onclick={() => handleSyncNow(item.id, item.url)} disabled={isLoading.syncing}>
					<RefreshCwIcon class="size-4" />
					{m.gitops_sync_now()}
				</DropdownMenu.Item>
				<DropdownMenu.Item onclick={() => onEditRepository(item)}>
					<PencilIcon class="size-4" />
					{m.common_edit()}
				</DropdownMenu.Item>
				<DropdownMenu.Item variant="destructive" onclick={() => handleDeleteOne(item.id, item.url)} disabled={isLoading.removing}>
					<Trash2Icon class="size-4" />
					{m.common_remove()}
				</DropdownMenu.Item>
			</DropdownMenu.Group>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/snippet}

{#snippet GitOpsMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: GitOpsRepository;
	mobileFieldVisibility: Record<string, boolean>;
})}
	<UniversalMobileCard
		{item}
		icon={{ component: GitBranchIcon, variant: 'blue' as const }}
		title={(item) => item.url}
		subtitle={(item) => ((mobileFieldVisibility.branch ?? true) ? `Branch: ${item.branch}` : null)}
		badges={[{ variant: 'blue' as const, text: 'GitOps' }]}
		fields={[
			{
				label: m.gitops_compose_path(),
				getValue: (item: GitOpsRepository) => item.composePath,
				icon: LinkIcon,
				iconVariant: 'gray' as const,
				show: (mobileFieldVisibility.composePath ?? true) && item.composePath !== undefined
			},
			{
				label: m.gitops_auto_sync(),
				getValue: (item: GitOpsRepository) => (item.autoSync ? 'Enabled' : 'Disabled'),
				show: mobileFieldVisibility.autoSync ?? true
			},
			{
				label: m.gitops_last_synced(),
				getValue: (item: GitOpsRepository) =>
					item.lastSyncedAt ? format(new Date(item.lastSyncedAt), 'MMM d, yyyy HH:mm') : 'Never',
				show: mobileFieldVisibility.lastSyncedAt ?? true
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

<div>
	<ArcaneTable
		persistKey="arcane-gitops-table"
		items={repositories}
		bind:requestOptions
		bind:selectedIds
		bind:mobileFieldVisibility
		onRemoveSelected={(ids) => handleDeleteSelected(ids)}
		onRefresh={async (options) => (repositories = await gitopsRepositoryService.getRepositories(options))}
		{columns}
		{mobileFields}
		rowActions={RowActions}
		mobileCard={GitOpsMobileCardSnippet}
	/>
</div>
