<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import EllipsisIcon from '@lucide/svelte/icons/ellipsis';
	import PencilIcon from '@lucide/svelte/icons/pencil';
	import TestTubeIcon from '@lucide/svelte/icons/test-tube';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { toast } from 'svelte-sonner';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import type { ContainerRegistry } from '$lib/types/container-registry.type';
	import type { ColumnSpec } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table/index.js';
	import PackageIcon from '@lucide/svelte/icons/package';
	import UserIcon from '@lucide/svelte/icons/user';
	import LinkIcon from '@lucide/svelte/icons/link';
	import { format } from 'date-fns';
	import { m } from '$lib/paraglide/messages';
	import { containerRegistryService } from '$lib/services/container-registry-service';

	let {
		registries = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable(),
		onEditRegistry
	}: {
		registries: Paginated<ContainerRegistry>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
		onEditRegistry: (registry: ContainerRegistry) => void;
	} = $props();

	let isLoading = $state({
		removing: false,
		testing: false
	});

	function getRegistryDisplayName(url: string) {
		if (!url || url === 'docker.io') return m.registry_docker_hub();
		if (url.includes('ghcr.io')) return m.registry_github_container_registry();
		if (url.includes('gcr.io')) return m.registry_google_container_registry();
		if (url.includes('quay.io')) return m.registry_quay_io();
		return url;
	}

	async function handleDeleteSelected(ids: string[]) {
		if (!ids?.length) return;

		openConfirmDialog({
			title: m.registries_remove_selected_title({ count: ids.length }),
			message: m.registries_remove_selected_message({ count: ids.length }),
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;

					let successCount = 0;
					let failureCount = 0;
					for (const id of ids) {
						const reg = registries.data.find((r) => r.id === id);
						const result = await tryCatch(containerRegistryService.deleteRegistry(id));
						if (result.error) {
							failureCount++;
							toast.error(m.registries_delete_failed({ url: reg?.url ?? m.common_unknown() }));
						} else {
							successCount++;
						}
					}

					if (successCount > 0) {
						toast.success(m.registries_bulk_remove_success({ count: successCount }));
						registries = await containerRegistryService.getRegistries(requestOptions);
					}
					if (failureCount > 0) toast.error(m.registries_bulk_remove_failed({ count: failureCount }));

					selectedIds = [];
					isLoading.removing = false;
				}
			}
		});
	}

	async function handleDeleteOne(id: string, url: string) {
		const safeUrl = url ?? m.common_unknown();
		openConfirmDialog({
			title: m.common_remove_title({ resource: m.resource_registry() }),
			message: m.registries_remove_message({ url: safeUrl }),
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;

					const result = await tryCatch(containerRegistryService.deleteRegistry(id));
					handleApiResultWithCallbacks({
						result,
						message: m.registries_delete_failed({ url: safeUrl }),
						setLoadingState: () => {},
						onSuccess: async () => {
							toast.success(m.common_delete_success({ resource: `${m.resource_registry()} "${safeUrl}"` }));
							registries = await containerRegistryService.getRegistries(requestOptions);
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
		const result = await tryCatch(containerRegistryService.testRegistry(id));
		handleApiResultWithCallbacks({
			result,
			message: m.registries_test_failed({ url: safeUrl }),
			setLoadingState: () => {},
			onSuccess: (resp) => {
				const msg = (resp as any)?.message ?? m.common_unknown();
				toast.success(m.registries_test_success({ url: safeUrl, message: msg }));
			}
		});
		isLoading.testing = false;
	}

	const columns = [
		{ accessorKey: 'id', title: m.common_id(), hidden: true },
		{
			accessorKey: 'url',
			title: m.registries_url(),
			sortable: true,
			cell: UrlCell
		},
		{
			accessorKey: 'username',
			title: m.common_username(),
			sortable: true,
			cell: UsernameCell
		},
		{
			accessorKey: 'description',
			title: m.common_description(),
			sortable: true,
			cell: DescriptionCell
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
	] satisfies ColumnSpec<ContainerRegistry>[];

	const mobileFields = [
		{ id: 'id', label: m.common_id(), defaultVisible: true },
		{ id: 'username', label: m.common_username(), defaultVisible: true },
		{ id: 'description', label: m.common_description(), defaultVisible: true }
	];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet UrlCell({ item }: { item: ContainerRegistry })}
	<div class="flex flex-col">
		<span class="font-medium">{item.url || 'docker.io'}</span>
		<span class="text-muted-foreground text-xs">{getRegistryDisplayName(item.url)}</span>
	</div>
{/snippet}

{#snippet UsernameCell({ value }: { value: unknown })}
	<span class="font-mono text-sm">{String(value ?? m.common_na())}</span>
{/snippet}

{#snippet DescriptionCell({ value }: { value: unknown })}
	<span class="text-muted-foreground text-sm">{String(value ?? m.common_no_description())}</span>
{/snippet}

{#snippet StatusCell({ value }: { value: unknown })}
	{@const enabled = Boolean(value)}
	<StatusBadge variant={enabled ? 'green' : 'red'} text={enabled ? m.common_enabled() : m.common_disabled()} />
{/snippet}

{#snippet CreatedCell({ value }: { value: unknown })}
	<span class="text-sm">{value ? format(new Date(String(value)), 'PP p') : m.common_na()}</span>
{/snippet}

{#snippet RegistryMobileCardSnippet({
	row,
	item,
	mobileFieldVisibility
}: {
	row: any;
	item: ContainerRegistry;
	mobileFieldVisibility: Record<string, boolean>;
})}
	<UniversalMobileCard
		{item}
		icon={{ component: PackageIcon, variant: 'purple' as const }}
		title={(item) => item.url}
		subtitle={(item) => ((mobileFieldVisibility.id ?? true) ? item.id : null)}
		badges={[{ variant: 'purple' as const, text: m.common_registry() }]}
		fields={[
			{
				label: m.common_username(),
				getValue: (item: ContainerRegistry) => item.username,
				icon: UserIcon,
				iconVariant: 'gray' as const,
				show: (mobileFieldVisibility.username ?? true) && item.username !== undefined
			},
			{
				label: m.common_description(),
				getValue: (item: ContainerRegistry) => item.description,
				icon: LinkIcon,
				iconVariant: 'gray' as const,
				show: (mobileFieldVisibility.description ?? true) && item.description !== undefined
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: ContainerRegistry })}
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
					{m.registries_test_connection()}
				</DropdownMenu.Item>
				<DropdownMenu.Item onclick={() => onEditRegistry(item)}>
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

<div>
	<ArcaneTable
		persistKey="arcane-registries-table"
		items={registries}
		bind:requestOptions
		bind:selectedIds
		bind:mobileFieldVisibility
		onRemoveSelected={(ids) => handleDeleteSelected(ids)}
		onRefresh={async (options) => (registries = await containerRegistryService.getRegistries(options))}
		{columns}
		{mobileFields}
		rowActions={RowActions}
		mobileCard={RegistryMobileCardSnippet}
	/>
</div>
