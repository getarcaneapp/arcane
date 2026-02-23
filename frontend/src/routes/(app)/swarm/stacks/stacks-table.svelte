<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import type { ColumnSpec, MobileFieldVisibility } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table';
	import { LayersIcon, EllipsisIcon, InspectIcon, TrashIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { swarmService } from '$lib/services/swarm-service';
	import type { SwarmStackSummary } from '$lib/types/swarm.type';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import { formatDistanceToNow } from 'date-fns';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { toast } from 'svelte-sonner';
	import { tryCatch } from '$lib/utils/try-catch';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { goto } from '$app/navigation';

	let {
		stacks = $bindable(),
		requestOptions = $bindable()
	}: {
		stacks: Paginated<SwarmStackSummary>;
		requestOptions: SearchPaginationSortRequest;
	} = $props();

	let isLoading = $state(false);

	function formatTimestamp(timestamp: string) {
		if (!timestamp) return m.common_unknown();
		return formatDistanceToNow(new Date(timestamp), { addSuffix: true });
	}

	function inspectStack(stack: SwarmStackSummary) {
		goto(`/swarm/stacks/${encodeURIComponent(stack.name)}`);
	}

	function handleDelete(stack: SwarmStackSummary) {
		openConfirmDialog({
			title: m.common_delete_title({ resource: m.swarm_stack() }),
			message: m.common_delete_confirm({ resource: m.swarm_stack() }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					handleApiResultWithCallbacks({
						result: await tryCatch(swarmService.removeStack(stack.name)),
						message: m.common_delete_failed({ resource: `${m.swarm_stack()} "${stack.name}"` }),
						setLoadingState: (v) => (isLoading = v),
						onSuccess: async () => {
							toast.success(m.common_delete_success({ resource: `${m.swarm_stack()} "${stack.name}"` }));
							stacks = await swarmService.getStacks(requestOptions);
						}
					});
				}
			}
		});
	}

	const columns = [
		{ accessorKey: 'id', title: m.common_id(), hidden: true },
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'services', title: m.services(), sortable: true },
		{ accessorKey: 'createdAt', title: m.common_created(), sortable: true, cell: CreatedCell },
		{ accessorKey: 'updatedAt', title: m.common_updated(), sortable: true, cell: UpdatedCell }
	] satisfies ColumnSpec<SwarmStackSummary>[];

	const mobileFields = [
		{ id: 'services', label: m.services(), defaultVisible: true },
		{ id: 'createdAt', label: m.common_created(), defaultVisible: true },
		{ id: 'updatedAt', label: m.common_updated(), defaultVisible: false }
	];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet NameCell({ item }: { item: SwarmStackSummary })}
	<a href="/swarm/stacks/{encodeURIComponent(item.name)}" class="text-primary text-sm font-medium hover:underline">
		{item.name}
	</a>
{/snippet}

{#snippet CreatedCell({ value }: { value: unknown })}
	<span class="text-sm">{formatTimestamp(String(value ?? ''))}</span>
{/snippet}

{#snippet UpdatedCell({ value }: { value: unknown })}
	<span class="text-sm">{formatTimestamp(String(value ?? ''))}</span>
{/snippet}

{#snippet StackMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: SwarmStackSummary;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={() => ({
			component: LayersIcon,
			variant: 'purple'
		})}
		title={(item: SwarmStackSummary) => item.name}
		subtitle={(item: SwarmStackSummary) => ((mobileFieldVisibility.createdAt ?? true) ? formatTimestamp(item.createdAt) : null)}
		fields={[
			{
				label: m.services(),
				getValue: (item: SwarmStackSummary) => String(item.services),
				icon: LayersIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.services ?? true
			},
			{
				label: m.common_updated(),
				getValue: (item: SwarmStackSummary) => formatTimestamp(item.updatedAt),
				icon: LayersIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.updatedAt ?? false
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: SwarmStackSummary })}
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
				<DropdownMenu.Item onclick={() => inspectStack(item)}>
					<InspectIcon class="size-4" />
					{m.common_inspect()}
				</DropdownMenu.Item>
				<DropdownMenu.Separator />
				<DropdownMenu.Item variant="destructive" onclick={() => handleDelete(item)} disabled={isLoading}>
					<TrashIcon class="size-4" />
					{m.common_delete()}
				</DropdownMenu.Item>
			</DropdownMenu.Group>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/snippet}

<ArcaneTable
	persistKey="arcane-swarm-stacks-table"
	items={stacks}
	bind:requestOptions
	bind:mobileFieldVisibility
	selectionDisabled={true}
	onRefresh={async (options) => (stacks = await swarmService.getStacks(options))}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={StackMobileCardSnippet}
/>
