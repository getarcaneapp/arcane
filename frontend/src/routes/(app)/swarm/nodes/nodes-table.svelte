<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import type { ColumnSpec, MobileFieldVisibility } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table';
	import { UsersIcon, EnvironmentsIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { swarmService } from '$lib/services/swarm-service';
	import type { SwarmNodeSummary } from '$lib/types/swarm.type';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { capitalizeFirstLetter } from '$lib/utils/string.utils';

	let {
		nodes = $bindable(),
		requestOptions = $bindable()
	}: {
		nodes: Paginated<SwarmNodeSummary>;
		requestOptions: SearchPaginationSortRequest;
	} = $props();

	function statusVariant(state: string): 'green' | 'red' | 'amber' | 'gray' {
		if (state === 'ready') return 'green';
		if (state === 'down') return 'red';
		if (state === 'unknown') return 'amber';
		return 'gray';
	}

	function availabilityVariant(state: string): 'green' | 'amber' | 'red' | 'gray' {
		if (state === 'active') return 'green';
		if (state === 'pause') return 'amber';
		if (state === 'drain') return 'red';
		return 'gray';
	}

	const columns = [
		{ accessorKey: 'id', title: m.common_id(), hidden: true },
		{ accessorKey: 'hostname', title: m.swarm_hostname(), sortable: true },
		{ accessorKey: 'role', title: m.common_role(), sortable: true, cell: RoleCell },
		{ accessorKey: 'status', title: m.common_status(), sortable: true, cell: StatusCell },
		{ accessorKey: 'availability', title: m.swarm_availability(), sortable: true, cell: AvailabilityCell },
		{ accessorKey: 'engineVersion', title: m.swarm_engine_version(), sortable: true }
	] satisfies ColumnSpec<SwarmNodeSummary>[];

	const mobileFields = [
		{ id: 'role', label: m.common_role(), defaultVisible: true },
		{ id: 'status', label: m.common_status(), defaultVisible: true },
		{ id: 'availability', label: m.swarm_availability(), defaultVisible: true },
		{ id: 'engineVersion', label: m.swarm_engine_version(), defaultVisible: false }
	];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet RoleCell({ value }: { value: unknown })}
	<span class="text-sm">{capitalizeFirstLetter(String(value ?? ''))}</span>
{/snippet}

{#snippet StatusCell({ value }: { value: unknown })}
	<StatusBadge text={String(value ?? m.common_unknown())} variant={statusVariant(String(value ?? ''))} />
{/snippet}

{#snippet AvailabilityCell({ value }: { value: unknown })}
	<StatusBadge text={String(value ?? m.common_unknown())} variant={availabilityVariant(String(value ?? ''))} />
{/snippet}

{#snippet NodeMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: SwarmNodeSummary;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={() => ({
			component: UsersIcon,
			variant: item.role === 'manager' ? 'purple' : 'blue'
		})}
		title={(item: SwarmNodeSummary) => item.hostname}
		subtitle={(item: SwarmNodeSummary) => ((mobileFieldVisibility.engineVersion ?? false) ? (item.engineVersion ?? '') : null)}
		badges={[
			(item: SwarmNodeSummary) =>
				(mobileFieldVisibility.status ?? true) ? { variant: statusVariant(item.status), text: item.status } : null
		]}
		fields={[
			{
				label: m.common_role(),
				getValue: (item: SwarmNodeSummary) => capitalizeFirstLetter(item.role),
				icon: EnvironmentsIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.role ?? true
			},
			{
				label: m.swarm_availability(),
				getValue: (item: SwarmNodeSummary) => capitalizeFirstLetter(item.availability),
				icon: EnvironmentsIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.availability ?? true
			}
		]}
	/>
{/snippet}

<ArcaneTable
	persistKey="arcane-swarm-nodes-table"
	items={nodes}
	bind:requestOptions
	bind:mobileFieldVisibility
	selectionDisabled={true}
	onRefresh={async (options) => (nodes = await swarmService.getNodes(options))}
	{columns}
	{mobileFields}
	mobileCard={NodeMobileCardSnippet}
/>
