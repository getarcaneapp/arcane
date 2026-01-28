<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import type { ColumnSpec, MobileFieldVisibility } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table';
	import { LayersIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { swarmService } from '$lib/services/swarm-service';
	import type { SwarmStackSummary } from '$lib/types/swarm.type';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import { formatDistanceToNow } from 'date-fns';

	let {
		stacks = $bindable(),
		requestOptions = $bindable()
	}: {
		stacks: Paginated<SwarmStackSummary>;
		requestOptions: SearchPaginationSortRequest;
	} = $props();

	function formatTimestamp(timestamp: string) {
		if (!timestamp) return m.common_unknown();
		return formatDistanceToNow(new Date(timestamp), { addSuffix: true });
	}

	const columns = [
		{ accessorKey: 'id', title: m.common_id(), hidden: true },
		{ accessorKey: 'name', title: m.common_name(), sortable: true },
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
	/>
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
	mobileCard={StackMobileCardSnippet}
/>
