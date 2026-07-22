<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import type { ColumnSpec, MobileFieldVisibility } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table';
	import { DockIcon, GlobeIcon, TrashIcon, NetworksIcon, InspectIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { swarmService } from '$lib/services/swarm-service';
	import type { SwarmServiceSummary, SwarmServicePort } from '$lib/types/swarm';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/shared';
	import { Badge } from '$lib/components/ui/badge';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import RowActionsMenu from '$lib/components/arcane-table/row-actions-menu.svelte';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { toast } from 'svelte-sonner';
	import { tryCatch } from '$lib/utils/api';
	import { handleApiResultWithCallbacks } from '$lib/utils/api';
	import { goto } from '$app/navigation';
	import { getSwarmServiceModeLabel, getSwarmServiceModeVariant } from '$lib/utils/docker';
	import IfPermitted from '$lib/components/if-permitted.svelte';

	let {
		services = $bindable(),
		requestOptions = $bindable(),
		fetchServices = (options: SearchPaginationSortRequest) => swarmService.getServices(options),
		persistKey = 'arcane-swarm-services-table'
	}: {
		services: Paginated<SwarmServiceSummary>;
		requestOptions: SearchPaginationSortRequest;
		fetchServices?: (options: SearchPaginationSortRequest) => Promise<Paginated<SwarmServiceSummary>>;
		persistKey?: string;
	} = $props();

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

	function inspectService(service: SwarmServiceSummary) {
		goto(`/swarm/services/${service.id}`);
	}

	function handleDelete(service: SwarmServiceSummary) {
		openConfirmDialog({
			title: m.common_delete_title({ resource: m.swarm_service() }),
			message: m.common_delete_confirm({ resource: m.swarm_service() }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					handleApiResultWithCallbacks({
						result: await tryCatch(swarmService.removeService(service.id)),
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

	const columns = [
		{ accessorKey: 'id', title: m.common_id(), hidden: true },
		{ accessorKey: 'stackName', title: m.swarm_stack(), sortable: true, cell: StackCell },
		{ accessorKey: 'name', title: m.swarm_service(), sortable: true, cell: NameCell },
		{ accessorKey: 'mode', title: m.swarm_mode(), sortable: true, cell: ModeCell },
		{ accessorKey: 'replicas', title: m.swarm_replicas(), sortable: true, cell: ReplicasCell },
		{
			id: 'nodes',
			accessorFn: (item: SwarmServiceSummary) => item.nodes,
			title: m.nodes(),
			cell: NodesCell
		},
		{
			id: 'networks',
			accessorFn: (item: SwarmServiceSummary) => item.networks,
			title: m.resource_networks_cap(),
			cell: NetworksCell
		},
		{ accessorKey: 'ports', title: m.common_ports(), cell: PortsCell }
	] satisfies ColumnSpec<SwarmServiceSummary>[];

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

{#snippet NameCell({ item }: { item: SwarmServiceSummary })}
	<a href="/swarm/services/{item.id}" class="text-sm font-medium text-primary hover:underline">
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

{#snippet ReplicasCell({ item }: { item: SwarmServiceSummary })}
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

{#snippet NodesCell({ item }: { item: SwarmServiceSummary })}
	{@render OverflowCell({ items: item.nodes })}
{/snippet}

{#snippet NetworksCell({ item }: { item: SwarmServiceSummary })}
	{@render OverflowCell({ items: item.networks })}
{/snippet}

{#snippet PortsCell({ item }: { item: SwarmServiceSummary })}
	{@render OverflowCell({ items: formatPortsList(item.ports) })}
{/snippet}

{#snippet ServiceMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: SwarmServiceSummary;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={() => ({
			component: DockIcon,
			variant: modeIconVariant(item.mode)
		})}
		title={(item: SwarmServiceSummary) => getShortName(item.name, item.stackName)}
		subtitle={(item: SwarmServiceSummary) => ((mobileFieldVisibility['stackName'] ?? true) ? (item.stackName ?? null) : null)}
		badges={[
			(item: SwarmServiceSummary) =>
				(mobileFieldVisibility['mode'] ?? true)
					? { variant: getSwarmServiceModeVariant(item.mode), text: getSwarmServiceModeLabel(item.mode) }
					: null
		]}
		fields={[
			{
				label: m.swarm_replicas(),
				getValue: (item: SwarmServiceSummary) => `${item.runningReplicas} / ${item.replicas}`,
				icon: GlobeIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['replicas'] ?? true
			},
			{
				label: m.nodes(),
				getValue: (item: SwarmServiceSummary) =>
					item.nodes?.length
						? item.nodes.slice(0, 3).join(', ') + (item.nodes.length > 3 ? ` +${item.nodes.length - 3}` : '')
						: m.common_na(),
				icon: NetworksIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['nodes'] ?? true
			},
			{
				label: m.resource_networks_cap(),
				getValue: (item: SwarmServiceSummary) => (item.networks?.length ? item.networks.join(', ') : m.common_na()),
				icon: NetworksIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['networks'] ?? false
			},
			{
				label: m.common_ports(),
				getValue: (item: SwarmServiceSummary) => formatPortsList(item.ports).join(', ') || m.common_na(),
				icon: GlobeIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['ports'] ?? false
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: SwarmServiceSummary })}
	<RowActionsMenu triggerClass="relative size-8 p-0" iconClass="">
		<DropdownMenu.Item onclick={() => inspectService(item)}>
			<InspectIcon class="size-4" />
			{m.common_inspect()}
		</DropdownMenu.Item>
		<DropdownMenu.Separator />
		<IfPermitted perm="swarm:services">
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
