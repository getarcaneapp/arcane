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
	import type { GitRepository } from '$lib/types/gitops.type';
	import type { ColumnSpec } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table/index.js';
	import { format } from 'date-fns';
	import { m } from '$lib/paraglide/messages';
	import { gitRepositoryService } from '$lib/services/git-repository-service';
	import {
		EllipsisIcon,
		EditIcon as PencilIcon,
		TestIcon as TestTubeIcon,
		TrashIcon as Trash2Icon,
		GitBranchIcon,
		ApiKeyIcon as KeyIcon,
		ExternalLinkIcon as LinkIcon
	} from '$lib/icons';

	type FieldVisibility = Record<string, boolean>;

	let {
		repositories = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable(),
		onEditRepository
	}: {
		repositories: Paginated<GitRepository>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
		onEditRepository: (repository: GitRepository) => void;
	} = $props();

	let isLoading = $state({
		removing: false,
		testing: false
	});
	let mobileFieldVisibility = $state<Record<string, boolean>>({});

	async function handleDeleteSelected(ids: string[]) {
		if (!ids?.length) return;

		openConfirmDialog({
			title: m.common_remove_title({ resource: `${ids.length} ${m.resource_repository()}(s)` }),
			message: m.common_remove_message({ resource: `${ids.length} ${m.resource_repository()}(s)` }),
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;

					let successCount = 0;
					let failureCount = 0;
					for (const id of ids) {
						const repo = repositories.data.find((r) => r.id === id);
						const result = await tryCatch(gitRepositoryService.deleteRepository(id));
						if (result.error) {
							failureCount++;
							toast.error(m.common_delete_failed({ resource: repo?.name ?? m.common_unknown() }));
						} else {
							successCount++;
						}
					}

					if (successCount > 0) {
						toast.success(m.common_delete_success({ resource: `${successCount} ${m.resource_repository()}(s)` }));
						repositories = await gitRepositoryService.getRepositories(requestOptions);
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
			title: m.git_repository_remove_confirm(),
			message: m.git_repository_remove_message(),
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;

					const result = await tryCatch(gitRepositoryService.deleteRepository(id));
					handleApiResultWithCallbacks({
						result,
						message: m.common_delete_failed({ resource: safeName }),
						setLoadingState: () => {},
						onSuccess: async () => {
							toast.success(m.common_delete_success({ resource: `${m.resource_repository()} "${safeName}"` }));
							repositories = await gitRepositoryService.getRepositories(requestOptions);
						}
					});

					isLoading.removing = false;
				}
			}
		});
	}

	async function handleTest(id: string, name: string) {
		isLoading.testing = true;
		const safeName = name ?? m.common_unknown();
		const result = await tryCatch(gitRepositoryService.testRepository(id));
		handleApiResultWithCallbacks({
			result,
			message: m.common_test_failed({ resource: safeName }),
			setLoadingState: () => {},
			onSuccess: () => {
				toast.success(m.common_test_success({ resource: safeName }));
			}
		});
		isLoading.testing = false;
	}

	const columns = [
		{ accessorKey: 'id', title: m.common_id(), hidden: true },
		{
			accessorKey: 'name',
			title: m.git_repository_name(),
			sortable: true,
			cell: NameCell
		},
		{
			accessorKey: 'url',
			title: m.git_repository_url(),
			sortable: true,
			cell: UrlCell
		},
		{
			accessorKey: 'authType',
			title: m.git_repository_auth_type(),
			sortable: true,
			cell: AuthTypeCell
		},
		{
			accessorKey: 'enabled',
			title: m.common_status(),
			sortable: true,
			cell: StatusCell
		},
		{
			accessorKey: 'createdAt',
			title: m.common_created(),
			sortable: true,
			cell: CreatedCell
		}
	] satisfies ColumnSpec<GitRepository>[];

	const mobileFields = [
		{ id: 'id', label: m.common_id(), defaultVisible: false },
		{ id: 'name', label: m.git_repository_name(), defaultVisible: true },
		{ id: 'url', label: m.git_repository_url(), defaultVisible: true },
		{ id: 'authType', label: m.git_repository_auth_type(), defaultVisible: true },
		{ id: 'enabled', label: m.common_status(), defaultVisible: true },
		{ id: 'createdAt', label: m.common_created(), defaultVisible: true }
	];
</script>

{#snippet NameCell({ value }: { value: unknown })}
	<div class="flex items-center gap-2">
		<GitBranchIcon class="text-muted-foreground size-4" />
		<span class="font-medium">{value}</span>
	</div>
{/snippet}

{#snippet UrlCell({ value }: { value: unknown })}
	<code class="bg-muted text-muted-foreground rounded px-2 py-1 text-xs">{value}</code>
{/snippet}

{#snippet AuthTypeCell({ value }: { value: unknown })}
	{@const authType = String(value)}
	{#if authType === 'http'}
		<StatusBadge variant="blue" text={m.git_repository_auth_http()} />
	{:else if authType === 'ssh'}
		<StatusBadge variant="purple" text={m.git_repository_auth_ssh()} />
	{:else}
		<StatusBadge variant="gray" text={m.git_repository_auth_none()} />
	{/if}
{/snippet}

{#snippet StatusCell({ value }: { value: unknown })}
	{@const enabled = Boolean(value)}
	<StatusBadge variant={enabled ? 'green' : 'red'} text={enabled ? m.common_enabled() : m.common_disabled()} />
{/snippet}

{#snippet CreatedCell({ value }: { value: unknown })}
	<span class="text-sm">{value ? format(new Date(String(value)), 'PP p') : m.common_na()}</span>
{/snippet}

{#snippet RepositoryMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	row: any;
	item: GitRepository;
	mobileFieldVisibility: FieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={{ component: GitBranchIcon, variant: 'blue' as const }}
		title={(item) => item.name}
		subtitle={(item) => ((mobileFieldVisibility.id ?? false) ? item.id : item.url)}
		badges={[{ variant: 'blue' as const, text: m.resource_repository_cap() }]}
		fields={[
			{
				label: m.git_repository_url(),
				getValue: (item: GitRepository) => item.url,
				icon: LinkIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.url ?? true
			},
			{
				label: m.git_repository_auth_type(),
				getValue: (item: GitRepository) => item.authType,
				icon: KeyIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.authType ?? true
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: GitRepository })}
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
				<DropdownMenu.Item onclick={() => handleTest(item.id, item.name)} disabled={isLoading.testing}>
					<TestTubeIcon class="size-4" />
					{m.git_repository_test_connection()}
				</DropdownMenu.Item>
				<DropdownMenu.Item onclick={() => onEditRepository(item)}>
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
	persistKey="arcane-git-repositories-table"
	items={repositories}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	onRemoveSelected={(ids) => handleDeleteSelected(ids)}
	onRefresh={async (options) => (repositories = await gitRepositoryService.getRepositories(options))}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={RepositoryMobileCardSnippet}
/>
