<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import type { ColumnSpec, MobileFieldVisibility } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table';
	import { JobsIcon, ConnectionIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { swarmService } from '$lib/services/swarm-service';
	import type { SwarmTaskSummary } from '$lib/types/swarm.type';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';

	let {
		tasks = $bindable(),
		requestOptions = $bindable()
	}: {
		tasks: Paginated<SwarmTaskSummary>;
		requestOptions: SearchPaginationSortRequest;
	} = $props();

	function stateVariant(state: string): 'green' | 'amber' | 'red' | 'gray' {
		if (state === 'running') return 'green';
		if (state === 'pending' || state === 'starting') return 'amber';
		if (state === 'failed' || state === 'rejected' || state === 'shutdown') return 'red';
		return 'gray';
	}

	function iconVariant(state: string): 'emerald' | 'amber' | 'red' | 'gray' {
		if (state === 'running') return 'emerald';
		if (state === 'pending' || state === 'starting') return 'amber';
		if (state === 'failed' || state === 'rejected' || state === 'shutdown') return 'red';
		return 'gray';
	}

	const columns = [
		{ accessorKey: 'id', title: m.common_id(), hidden: true },
		{ accessorKey: 'name', title: m.common_name(), sortable: true },
		{ accessorKey: 'serviceName', title: m.swarm_service(), sortable: true },
		{ accessorKey: 'nodeName', title: m.swarm_node(), sortable: true },
		{ accessorKey: 'currentState', title: m.swarm_current_state(), sortable: true, cell: StateCell },
		{ accessorKey: 'desiredState', title: m.swarm_desired_state(), sortable: true, cell: DesiredStateCell }
	] satisfies ColumnSpec<SwarmTaskSummary>[];

	const mobileFields = [
		{ id: 'serviceName', label: m.swarm_service(), defaultVisible: true },
		{ id: 'nodeName', label: m.swarm_node(), defaultVisible: true },
		{ id: 'currentState', label: m.swarm_current_state(), defaultVisible: true },
		{ id: 'desiredState', label: m.swarm_desired_state(), defaultVisible: false }
	];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet StateCell({ value }: { value: unknown })}
	<StatusBadge text={String(value ?? m.common_unknown())} variant={stateVariant(String(value ?? ''))} />
{/snippet}

{#snippet DesiredStateCell({ value }: { value: unknown })}
	<StatusBadge text={String(value ?? m.common_unknown())} variant={stateVariant(String(value ?? ''))} />
{/snippet}

{#snippet TaskMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: SwarmTaskSummary;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={() => ({
			component: JobsIcon,
			variant: iconVariant(item.currentState)
		})}
		title={(item: SwarmTaskSummary) => item.name}
		subtitle={(item: SwarmTaskSummary) => ((mobileFieldVisibility.serviceName ?? true) ? item.serviceName : null)}
		badges={[
			(item: SwarmTaskSummary) =>
				(mobileFieldVisibility.currentState ?? true)
					? { variant: stateVariant(item.currentState), text: item.currentState }
					: null
		]}
		fields={[
			{
				label: m.swarm_node(),
				getValue: (item: SwarmTaskSummary) => item.nodeName,
				icon: ConnectionIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.nodeName ?? true
			},
			{
				label: m.swarm_desired_state(),
				getValue: (item: SwarmTaskSummary) => item.desiredState,
				icon: ConnectionIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.desiredState ?? false
			}
		]}
	/>
{/snippet}

<ArcaneTable
	persistKey="arcane-swarm-tasks-table"
	items={tasks}
	bind:requestOptions
	bind:mobileFieldVisibility
	selectionDisabled={true}
	onRefresh={async (options) => (tasks = await swarmService.getTasks(options))}
	{columns}
	{mobileFields}
	mobileCard={TaskMobileCardSnippet}
/>
