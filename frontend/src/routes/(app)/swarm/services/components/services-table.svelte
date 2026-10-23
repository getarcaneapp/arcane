<script lang="ts">
	import { goto } from '$app/navigation';
	import { toast } from 'svelte-sonner';

	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import { environmentColumns } from '#lib/components/arcane-table/environment-column.js';
	import type { ColumnSpec, MobileFieldVisibility } from '#lib/components/arcane-table/index.js';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { DockIcon, GlobeIcon, TrashIcon, NetworksIcon, InspectIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { swarmService, type SwarmServicesPaginatedResponse } from '#lib/services/swarm-service.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import type { SwarmServiceSummary, SwarmServicePort } from '#lib/types/swarm.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { getSwarmServiceModeLabel, getSwarmServiceModeVariant } from '#lib/utils/docker.js';
	import { environmentHref, type EnvironmentPage, type EnvironmentRow } from '#lib/utils/environments.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let {
		services = $bindable(),
		requestOptions = $bindable(),
		fetchServices,
		persistKey = 'arcane-swarm-services-table',
		showEnvironment = true
	}: {
		services: ServicePage;
		requestOptions: SearchPaginationSortRequest;
		fetchServices: (options: SearchPaginationSortRequest) => Promise<ServicePage>;
		persistKey?: string;
		showEnvironment?: boolean;
	} = $props();

	type ServiceRow = EnvironmentRow<SwarmServiceSummary>;
	type ServicePage = EnvironmentPage<SwarmServicesPaginatedResponse>;

	const MAX_OVERFLOW_ITEMS = 3;

	function formatPort(port: SwarmServicePort): string {
		const protocol = port.protocol || 'tcp';
		if (port.publishedPort) {
			return `${port.publishedPort}:${port.targetPort}/${protocol}`;
		}
		return `${port.targetPort}/${protocol}`;
	}

	function formatPortsList(ports?: SwarmServicePort[]): string[] {
		if (!ports || ports.length === 0) return [];
		return ports.map(formatPort);
	}

	function getShortName(name: string, stackName?: string | null): string {
		if (stackName && name.startsWith(`${stackName}_`)) {
			return name.slice(stackName.length + 1);
		}
		return name;
	}

	function modeIconVariant(mode: string): 'emerald' | 'blue' | 'amber' | 'purple' | 'gray' {
		if (mode === 'replicated') return 'blue';
		if (mode === 'global') return 'emerald';
		if (mode === 'replicated-job') return 'amber';
		if (mode === 'global-job') return 'purple';
		return 'gray';
	}

	let isLoading = $state({ remove: false });

	const isAnyLoading = $derived(Object.values(isLoading).some(Boolean));

	function inspectService(service: ServiceRow) {
		goto(environmentHref(`/swarm/services/${service.id}`, service.environmentId));
	}

	function handleDelete(service: ServiceRow) {
		openConfirmDialog({
			title: m.common_delete_title({ resource: m.swarm_service() }),
			message: m.common_delete_confirm({ resource: m.swarm_service() }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					isLoading.remove = true;
					await handleApiResultWithCallbacks({
						result: await tryCatch(swarmService.removeService(service.id, service.environmentId)),
						message: m.common_delete_failed({ resource: `${m.swarm_service()} "${service.name}"` }),
						setLoadingState: (v) => (isLoading.remove = v),
						onSuccess: async () => {
							toast.success(m.common_delete_success({ resource: `${m.swarm_service()} "${service.name}"` }));
							services = await fetchServices(requestOptions);
						}
					});
				}
			}
		});
	}

	const columns = $derived([
		{ accessorKey: 'id', title: m.common_id(), hidden: true },
		...(showEnvironment ? environmentColumns<ServiceRow>() : []),
		{ accessorKey: 'stackName', title: m.swarm_stack(), sortable: true, cell: StackCell },
		{ accessorKey: 'name', title: m.swarm_service(), sortable: true, cell: NameCell },
		{ accessorKey: 'mode', title: m.swarm_mode(), sortable: true, cell: ModeCell },
		{ accessorKey: 'replicas', title: m.swarm_replicas(), sortable: true, cell: ReplicasCell },
		{
			id: 'nodes',
			accessorFn: (item: ServiceRow) => item.nodes,
			title: m.nodes(),
			cell: NodesCell
		},
		{
			id: 'networks',
			accessorFn: (item: ServiceRow) => item.networks,
			title: m.resource_networks_cap(),
			cell: NetworksCell
		},
		{ accessorKey: 'ports', title: m.common_ports(), cell: PortsCell }
	] satisfies ColumnSpec<ServiceRow>[]);

	const mobileFields = [
		{ id: 'stackName', label: m.swarm_stack(), defaultVisible: true },
		{ id: 'mode', label: m.swarm_mode(), defaultVisible: true },
		{ id: 'replicas', label: m.swarm_replicas(), defaultVisible: true },
		{ id: 'nodes', label: m.nodes(), defaultVisible: true },
		{ id: 'networks', label: m.resource_networks_cap(), defaultVisible: false },
		{ id: 'ports', label: m.common_ports(), defaultVisible: false }
	];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet NameCell({ item }: { item: ServiceRow })}
	<a
		href={environmentHref(`/swarm/services/${item.id}`, item.environmentId)}
		class="text-sm font-medium text-primary hover:underline"
	>
		{getShortName(item.name, item.stackName)}
	</a>
{/snippet}

{#snippet ModeCell({ value }: { value: unknown })}
	<Badge variant={getSwarmServiceModeVariant(String(value ?? ''))} minWidth="20"
		>{getSwarmServiceModeLabel(String(value ?? ''))}</Badge
	>
{/snippet}

{#snippet StackCell({ value }: { value: unknown })}
	{#if value}
		<span class="text-sm">{String(value)}</span>
	{:else}
		<span class="text-sm text-muted-foreground">{m.common_na()}</span>
	{/if}
{/snippet}

{#snippet ReplicasCell({ item }: { item: ServiceRow })}
	<span class="font-mono text-sm">{item.runningReplicas} / {item.replicas}</span>
{/snippet}

{#snippet OverflowCell({ items }: { items: string[] })}
	{#if !items || items.length === 0}
		<span class="text-sm text-muted-foreground">{m.common_na()}</span>
	{:else}
		<div class="flex flex-col gap-0.5">
			{#each items.slice(0, MAX_OVERFLOW_ITEMS) as item (item)}
				<span class="max-w-45 truncate font-mono text-sm">{item}</span>
			{/each}
			{#if items.length > MAX_OVERFLOW_ITEMS}
				<span class="text-xs font-medium text-muted-foreground">
					{m.count_more({ count: items.length - MAX_OVERFLOW_ITEMS })}
				</span>
			{/if}
		</div>
	{/if}
{/snippet}

{#snippet NodesCell({ item }: { item: ServiceRow })}
	{@render OverflowCell({ items: item.nodes })}
{/snippet}

{#snippet NetworksCell({ item }: { item: ServiceRow })}
	{@render OverflowCell({ items: item.networks })}
{/snippet}

{#snippet PortsCell({ item }: { item: ServiceRow })}
	{@render OverflowCell({ items: formatPortsList(item.ports) })}
{/snippet}

{#snippet ServiceMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: ServiceRow;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={() => ({
			component: DockIcon,
			variant: modeIconVariant(item.mode)
		})}
		title={(item: ServiceRow) => getShortName(item.name, item.stackName)}
		subtitle={(item: ServiceRow) => ((mobileFieldVisibility['stackName'] ?? true) ? (item.stackName ?? null) : null)}
		badges={[
			(item: ServiceRow) =>
				(mobileFieldVisibility['mode'] ?? true)
					? { variant: getSwarmServiceModeVariant(item.mode), text: getSwarmServiceModeLabel(item.mode) }
					: null
		]}
		fields={[
			{
				label: m.swarm_replicas(),
				getValue: (item: ServiceRow) => `${item.runningReplicas} / ${item.replicas}`,
				icon: GlobeIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['replicas'] ?? true
			},
			{
				label: m.nodes(),
				getValue: (item: ServiceRow) =>
					item.nodes?.length
						? item.nodes.slice(0, 3).join(', ') + (item.nodes.length > 3 ? ` +${item.nodes.length - 3}` : '')
						: m.common_na(),
				icon: NetworksIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['nodes'] ?? true
			},
			{
				label: m.resource_networks_cap(),
				getValue: (item: ServiceRow) => (item.networks?.length ? item.networks.join(', ') : m.common_na()),
				icon: NetworksIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['networks'] ?? false
			},
			{
				label: m.common_ports(),
				getValue: (item: ServiceRow) => formatPortsList(item.ports).join(', ') || m.common_na(),
				icon: GlobeIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['ports'] ?? false
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: ServiceRow })}
	<RowActionsMenu triggerClass="relative size-8 p-0" iconClass="">
		<DropdownMenu.Item onclick={() => inspectService(item)}>
			<InspectIcon class="size-4" />
			{m.common_inspect()}
		</DropdownMenu.Item>
		<DropdownMenu.Separator />
		<IfPermitted perm="swarm:services" envId={item.environmentId}>
			<DropdownMenu.Item variant="destructive" onclick={() => handleDelete(item)} disabled={isAnyLoading}>
				<TrashIcon class="size-4" />
				{m.common_delete()}
			</DropdownMenu.Item>
		</IfPermitted>
	</RowActionsMenu>
{/snippet}

<ArcaneTable
	{persistKey}
	items={services}
	bind:requestOptions
	bind:mobileFieldVisibility
	selectionDisabled={true}
	onRefresh={async (options) => (services = await fetchServices(options))}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={ServiceMobileCardSnippet}
/>
