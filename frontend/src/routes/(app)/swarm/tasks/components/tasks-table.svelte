<script lang="ts">
	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import { environmentColumns } from '#lib/components/arcane-table/environment-column.js';
	import type { ColumnSpec, MobileFieldVisibility } from '#lib/components/arcane-table/index.js';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { JobsIcon, ConnectionIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { SwarmTasksPaginatedResponse } from '#lib/services/swarm-service.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import type { SwarmTaskSummary } from '#lib/types/swarm.js';
	import type { EnvironmentPage, EnvironmentRow } from '#lib/utils/environments.js';
	import { getSwarmTaskIconVariant, getSwarmTaskStateVariant } from '#lib/utils/swarm-tasks.js';

	let {
		tasks = $bindable(),
		requestOptions = $bindable(),
		fetchTasks,
		persistKey = 'arcane-swarm-tasks-table',
		showEnvironment = true
	}: {
		tasks: TaskPage;
		requestOptions: SearchPaginationSortRequest;
		fetchTasks: (options: SearchPaginationSortRequest) => Promise<TaskPage>;
		persistKey?: string;
		showEnvironment?: boolean;
	} = $props();

	type TaskRow = EnvironmentRow<SwarmTaskSummary>;
	type TaskPage = EnvironmentPage<SwarmTasksPaginatedResponse>;

	const columns = $derived([
		{ accessorKey: 'id', title: m.common_id(), hidden: true },
		...(showEnvironment ? environmentColumns<TaskRow>() : []),
		{ accessorKey: 'name', title: m.common_name(), sortable: true },
		{ accessorKey: 'serviceName', title: m.swarm_service(), sortable: true },
		{ accessorKey: 'nodeName', title: m.swarm_node(), sortable: true },
		{ accessorKey: 'currentState', title: m.swarm_current_state(), sortable: true, cell: StateCell },
		{ accessorKey: 'desiredState', title: m.swarm_desired_state(), sortable: true, cell: DesiredStateCell }
	] satisfies ColumnSpec<TaskRow>[]);

	const mobileFields = [
		{ id: 'serviceName', label: m.swarm_service(), defaultVisible: true },
		{ id: 'nodeName', label: m.swarm_node(), defaultVisible: true },
		{ id: 'currentState', label: m.swarm_current_state(), defaultVisible: true },
		{ id: 'desiredState', label: m.swarm_desired_state(), defaultVisible: false }
	];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet StateCell({ value }: { value: unknown })}
	<Badge variant={getSwarmTaskStateVariant(String(value ?? ''))} minWidth="20">{String(value ?? m.common_unknown())}</Badge>
{/snippet}

{#snippet DesiredStateCell({ value }: { value: unknown })}
	<Badge variant={getSwarmTaskStateVariant(String(value ?? ''))} minWidth="20">{String(value ?? m.common_unknown())}</Badge>
{/snippet}

{#snippet TaskMobileCardSnippet({ item, mobileFieldVisibility }: { item: TaskRow; mobileFieldVisibility: MobileFieldVisibility })}
	<UniversalMobileCard
		{item}
		icon={() => ({
			component: JobsIcon,
			variant: getSwarmTaskIconVariant(item.currentState)
		})}
		title={(item: TaskRow) => item.name}
		subtitle={(item: TaskRow) => ((mobileFieldVisibility['serviceName'] ?? true) ? item.serviceName : null)}
		badges={[
			(item: TaskRow) =>
				(mobileFieldVisibility['currentState'] ?? true)
					? { variant: getSwarmTaskStateVariant(item.currentState), text: item.currentState }
					: null
		]}
		fields={[
			{
				label: m.swarm_node(),
				getValue: (item: TaskRow) => item.nodeName,
				icon: ConnectionIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['nodeName'] ?? true
			},
			{
				label: m.swarm_desired_state(),
				getValue: (item: TaskRow) => item.desiredState,
				icon: ConnectionIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['desiredState'] ?? false
			}
		]}
	/>
{/snippet}

<ArcaneTable
	{persistKey}
	items={tasks}
	bind:requestOptions
	bind:mobileFieldVisibility
	selectionDisabled={true}
	onRefresh={async (options) => (tasks = await fetchTasks(options))}
	{columns}
	{mobileFields}
	mobileCard={TaskMobileCardSnippet}
/>
