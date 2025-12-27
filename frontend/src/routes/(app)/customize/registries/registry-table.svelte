<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { toast } from 'svelte-sonner';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import type { ContainerRegistry } from '$lib/types/container-registry.type';
	import type { ColumnSpec, MobileFieldVisibility } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table/index.js';
	import { format } from 'date-fns';
	import { m } from '$lib/paraglide/messages';
	import { containerRegistryService } from '$lib/services/container-registry-service';
	import { RegistryIcon, UserIcon, ExternalLinkIcon, EllipsisIcon, EditIcon, TrashIcon, TestIcon } from '$lib/icons';

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

	let removingId = $state<string | null>(null);
	let testingId = $state<string | null>(null);

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
					let successCount = 0;
					let failureCount = 0;
					for (const id of ids) {
						removingId = id;
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
					removingId = null;
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
					removingId = id;

					const result = await tryCatch(containerRegistryService.deleteRegistry(id));
					handleApiResultWithCallbacks({
						result,
						message: m.registries_delete_failed({ url: safeUrl }),
						setLoadingState: (value) => (value ? null : (removingId = null)),
						onSuccess: async () => {
							toast.success(m.common_delete_success({ resource: `${m.resource_registry()} "${safeUrl}"` }));
							registries = await containerRegistryService.getRegistries(requestOptions);
							removingId = null;
						}
					});
				}
			}
		});
	}

	async function handleTest(id: string, url: string) {
		testingId = id;
		const safeUrl = url ?? m.common_unknown();
		const result = await tryCatch(containerRegistryService.testRegistry(id));
		handleApiResultWithCallbacks({
			result,
			message: m.registries_test_failed({ url: safeUrl }),
			setLoadingState: (value) => (value ? null : (testingId = null)),
			onSuccess: (resp) => {
				const msg = (resp as any)?.message ?? m.common_unknown();
				toast.success(m.registries_test_success({ url: safeUrl, message: msg }));
				testingId = null;
			}
		});
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
		{ id: 'id', label: m.common_id(), defaultVisible: false },
		{ id: 'username', label: m.common_username(), defaultVisible: true },
		{ id: 'description', label: m.common_description(), defaultVisible: true },
		{ id: 'enabled', label: m.common_status(), defaultVisible: true },
		{ id: 'createdAt', label: m.common_created(), defaultVisible: true }
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
	item,
	mobileFieldVisibility
}: {
	item: ContainerRegistry;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={{ component: RegistryIcon, variant: 'purple' as const }}
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
				icon: ExternalLinkIcon,
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
				<ArcaneButton {...props} action="base" tone="ghost" size="icon" class="relative size-8 p-0">
					<span class="sr-only">{m.common_open_menu()}</span>
					<EllipsisIcon />
				</ArcaneButton>
			{/snippet}
		</DropdownMenu.Trigger>
		<DropdownMenu.Content align="end">
			<DropdownMenu.Group>
				<DropdownMenu.Item onclick={() => handleTest(item.id, item.url)} disabled={testingId === item.id}>
					{#if testingId === item.id}
						<Spinner class="size-4" />
					{:else}
						<TestIcon class="size-4" />
					{/if}
					{m.registries_test_connection()}
				</DropdownMenu.Item>
				<DropdownMenu.Item onclick={() => onEditRegistry(item)}>
					<EditIcon class="size-4" />
					{m.common_edit()}
				</DropdownMenu.Item>
				<DropdownMenu.Separator />
				<DropdownMenu.Item
					variant="destructive"
					onclick={() => handleDeleteOne(item.id, item.url)}
					disabled={removingId === item.id}
				>
					{#if removingId === item.id}
						<Spinner class="size-4" />
					{:else}
						<TrashIcon class="size-4" />
					{/if}
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
