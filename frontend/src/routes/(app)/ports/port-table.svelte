<script lang="ts">
	import { goto } from '$app/navigation';
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { UniversalMobileCard } from '$lib/components/arcane-table';
	import type { ColumnSpec, MobileFieldVisibility } from '$lib/components/arcane-table';
	import { ContainersIcon, ConnectionIcon, GlobeIcon, HashIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { portService } from '$lib/services/port-service';
	import type { SearchPaginationSortRequest, Paginated } from '$lib/types/pagination.type';
	import type { PortMappingDto } from '$lib/types/port.type';

	let {
		ports,
		selectedIds = $bindable(),
		requestOptions = $bindable(),
		onRefreshData
	}: {
		ports: Paginated<PortMappingDto>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
		onRefreshData?: (options: SearchPaginationSortRequest) => Promise<void>;
	} = $props();

	let mobileFieldVisibility = $state<Record<string, boolean>>({});

	function formatHostPort(port: PortMappingDto): string {
		return port.isPublished && port.hostPort ? String(port.hostPort) : m.ports_no_host_binding();
	}

	function formatHostIp(port: PortMappingDto): string {
		if (!port.isPublished) {
			return m.ports_no_host_binding();
		}
		return port.hostIp?.trim() || '0.0.0.0';
	}

	function mappingTitle(port: PortMappingDto): string {
		if (port.isPublished && port.hostPort) {
			return `${port.hostPort} -> ${port.containerPort}`;
		}
		return `${port.containerPort}/${port.protocol}`;
	}

	async function refreshPorts(options: SearchPaginationSortRequest = requestOptions): Promise<Paginated<PortMappingDto> | void> {
		if (onRefreshData) {
			await onRefreshData(options);
			return;
		}
		return await portService.getPorts(options);
	}

	const columns = [
		{ accessorKey: 'hostPort', title: m.ports_host_port(), sortable: true, cell: HostPortCell },
		{ accessorKey: 'containerPort', title: m.ports_container_port(), sortable: true, cell: ContainerPortCell },
		{ accessorKey: 'protocol', title: m.common_type(), sortable: true, cell: ProtocolCell },
		{ accessorKey: 'containerName', title: m.ports_container_name(), sortable: true, cell: ContainerCell },
		{ accessorKey: 'hostIp', title: m.ports_host_ip(), sortable: true, cell: HostIpCell },
		{ accessorKey: 'isPublished', title: m.common_status(), sortable: true, cell: PublishedCell }
	] satisfies ColumnSpec<PortMappingDto>[];

	const mobileFields = [
		{ id: 'hostPort', label: m.ports_host_port(), defaultVisible: true },
		{ id: 'containerPort', label: m.ports_container_port(), defaultVisible: true },
		{ id: 'protocol', label: m.common_type(), defaultVisible: true },
		{ id: 'containerName', label: m.ports_container_name(), defaultVisible: true },
		{ id: 'hostIp', label: m.ports_host_ip(), defaultVisible: true },
		{ id: 'isPublished', label: m.common_status(), defaultVisible: true }
	];
</script>

{#snippet HostPortCell({ item }: { item: PortMappingDto })}
	<span class:text-muted-foreground={!item.isPublished} class="font-mono text-sm">
		{formatHostPort(item)}
	</span>
{/snippet}

{#snippet ContainerPortCell({ item }: { item: PortMappingDto })}
	<span class="font-mono text-sm">{item.containerPort}</span>
{/snippet}

{#snippet ProtocolCell({ item }: { item: PortMappingDto })}
	<StatusBadge text={item.protocol.toUpperCase()} variant="gray" minWidth="none" />
{/snippet}

{#snippet ContainerCell({ item }: { item: PortMappingDto })}
	<a class="font-medium hover:underline" href="/containers/{item.containerId}">
		{item.containerName}
	</a>
{/snippet}

{#snippet HostIpCell({ item }: { item: PortMappingDto })}
	<span class:text-muted-foreground={!item.isPublished} class="font-mono text-sm">
		{formatHostIp(item)}
	</span>
{/snippet}

{#snippet PublishedCell({ item }: { item: PortMappingDto })}
	<StatusBadge
		text={item.isPublished ? m.ports_published_label() : m.ports_exposed_label()}
		variant={item.isPublished ? 'sky' : 'gray'}
		minWidth="none"
	/>
{/snippet}

{#snippet PortMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: PortMappingDto;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={(item: PortMappingDto) => ({
			component: item.isPublished ? ConnectionIcon : HashIcon,
			variant: item.isPublished ? 'blue' : 'gray'
		})}
		title={(item: PortMappingDto) => mappingTitle(item)}
		subtitle={(item: PortMappingDto) => ((mobileFieldVisibility.containerName ?? true) ? item.containerName : null)}
		badges={[
			(item: PortMappingDto) =>
				(mobileFieldVisibility.isPublished ?? true)
					? {
							variant: item.isPublished ? 'sky' : 'gray',
							text: item.isPublished ? m.ports_published_label() : m.ports_exposed_label()
						}
					: null
		]}
		fields={[
			{
				label: m.ports_container_port(),
				getValue: (item: PortMappingDto) => String(item.containerPort),
				icon: HashIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.containerPort ?? true
			},
			{
				label: m.common_type(),
				getValue: (item: PortMappingDto) => item.protocol.toUpperCase(),
				icon: ConnectionIcon,
				iconVariant: 'gray' as const,
				type: 'badge' as const,
				badgeVariant: 'gray' as const,
				show: mobileFieldVisibility.protocol ?? true
			},
			{
				label: m.ports_host_ip(),
				getValue: (item: PortMappingDto) => formatHostIp(item),
				icon: GlobeIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.hostIp ?? true
			},
			{
				label: m.ports_container_name(),
				getValue: (item: PortMappingDto) => item.containerName,
				icon: ContainersIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.containerName ?? true
			}
		]}
		onclick={() => void goto(`/containers/${item.containerId}`)}
	/>
{/snippet}

<ArcaneTable
	persistKey="arcane-ports-table"
	items={ports}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	onRefresh={async (options) => {
		requestOptions = options;
		return (await refreshPorts(options)) ?? ports;
	}}
	{columns}
	{mobileFields}
	mobileCard={PortMobileCardSnippet}
/>
