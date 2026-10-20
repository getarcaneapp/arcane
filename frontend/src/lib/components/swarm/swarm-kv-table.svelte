<script lang="ts">
	import { toast } from 'svelte-sonner';

	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import type { ColumnSpec, MobileFieldVisibility } from '#lib/components/arcane-table/index.js';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import { LabelBadges } from '#lib/components/badges/index.js';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { CalendarIcon, EditIcon, TrashIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { swarmService } from '#lib/services/swarm-service.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import type { SwarmKvSummary } from '#lib/types/swarm.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { formatRelativeTime } from '#lib/utils/formatting.js';
	import type { SwarmKvResource } from '#lib/utils/swarm-kv.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let {
		resource,
		items = $bindable(),
		requestOptions = $bindable(),
		onEdit
	}: {
		resource: SwarmKvResource;
		items: Paginated<SwarmKvSummary>;
		requestOptions: SearchPaginationSortRequest;
		onEdit: (item: SwarmKvSummary) => void;
	} = $props();

	let isDeleting = $state(false);
	let mobileFieldVisibility = $state<Record<string, boolean>>({});

	const canManage = $derived(hasPermission(resource.permission, environmentStore.selected?.id));

	async function reload(options: SearchPaginationSortRequest = requestOptions) {
		items = await swarmService.getKvItems(resource.kind, options);
		return items;
	}

	function handleDelete(item: SwarmKvSummary) {
		const name = item.spec.Name;
		openConfirmDialog({
			title: m.common_delete_title({ resource: resource.resource() }),
			message: resource.deleteConfirm(name),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					isDeleting = true;
					await handleApiResultWithCallbacks({
						result: await tryCatch(swarmService.removeKvItem(resource.kind, item.id)),
						message: resource.deleteFailed(name),
						setLoadingState: (value) => (isDeleting = value),
						onSuccess: async () => {
							toast.success(resource.deleteSuccess(name));
							await reload();
						}
					});
				}
			}
		});
	}

	const columns = $derived([
		{ accessorKey: 'id', title: m.common_id(), hidden: true },
		{ id: 'name', accessorFn: (item) => item.spec.Name, title: m.common_name(), sortable: true, cell: NameCell },
		...(resource.kind === 'secrets'
			? [
					{
						id: 'driver',
						accessorFn: (item: SwarmKvSummary) => item.spec.Driver?.Name ?? '',
						title: m.common_driver(),
						cell: DriverCell
					}
				]
			: []),
		{ id: 'labels', accessorFn: (item) => item.spec.Labels, title: m.common_labels(), cell: LabelsCell },
		{ accessorKey: 'createdAt', title: m.common_created(), sortable: true, cell: TimeCell },
		{ accessorKey: 'updatedAt', title: m.common_updated(), sortable: true, cell: TimeCell }
	] satisfies ColumnSpec<SwarmKvSummary>[]);

	const mobileFields = [
		{ id: 'labels', label: m.common_labels(), defaultVisible: true },
		{ id: 'createdAt', label: m.common_created(), defaultVisible: true }
	];
</script>

{#snippet NameCell({ item }: { item: SwarmKvSummary })}
	<button type="button" class="font-medium hover:underline" onclick={() => onEdit(item)}>{item.spec.Name}</button>
{/snippet}

{#snippet DriverCell({ item }: { item: SwarmKvSummary })}
	<span class="text-sm text-muted-foreground">{item.spec.Driver?.Name || '-'}</span>
{/snippet}

{#snippet LabelsCell({ item }: { item: SwarmKvSummary })}
	<LabelBadges labels={item.spec.Labels} />
{/snippet}

{#snippet TimeCell({ value }: { value: unknown })}
	<span class="text-sm">{formatRelativeTime(String(value ?? '')) || m.common_unknown()}</span>
{/snippet}

{#snippet MobileCard({ item, mobileFieldVisibility }: { item: SwarmKvSummary; mobileFieldVisibility: MobileFieldVisibility })}
	<UniversalMobileCard
		{item}
		icon={{ component: resource.icon, variant: 'purple' }}
		title={(item: SwarmKvSummary) => item.spec.Name}
		subtitle={(item: SwarmKvSummary) =>
			(mobileFieldVisibility['labels'] ?? true) ? Object.keys(item.spec.Labels ?? {}).join(', ') || null : null}
		footer={(mobileFieldVisibility['createdAt'] ?? true)
			? {
					label: m.common_created(),
					getValue: (item: SwarmKvSummary) => formatRelativeTime(item.createdAt) || m.common_unknown(),
					icon: CalendarIcon
				}
			: undefined}
		rowActions={RowActions}
		onclick={() => onEdit(item)}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: SwarmKvSummary })}
	<RowActionsMenu>
		<DropdownMenu.Item onclick={() => onEdit(item)}>
			<EditIcon class="size-4" />
			{m.common_edit()}
		</DropdownMenu.Item>
		{#if canManage}
			<DropdownMenu.Separator />
			<DropdownMenu.Item variant="destructive" onclick={() => handleDelete(item)} disabled={isDeleting}>
				<TrashIcon class="size-4" />
				{m.common_delete()}
			</DropdownMenu.Item>
		{/if}
	</RowActionsMenu>
{/snippet}

<ArcaneTable
	persistKey={resource.persistKey}
	{items}
	bind:requestOptions
	bind:mobileFieldVisibility
	selectionDisabled={true}
	onRefresh={async (options) => {
		requestOptions = options;
		return reload(options);
	}}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={MobileCard}
/>
