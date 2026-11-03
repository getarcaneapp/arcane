<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';

	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import { environmentColumns } from '#lib/components/arcane-table/environment-column.js';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import type { ColumnSpec, MobileFieldVisibility } from '#lib/components/arcane-table/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { ContainersIcon, ConnectionIcon, GlobeIcon, HashIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { PortMapping } from '#lib/types/port.js';
	import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
	import type { PortsPaginatedResponse } from '#lib/types/ui/port.js';
	import type { EnvironmentPage, EnvironmentRow } from '#lib/utils/environments.js';

	type PortRow = EnvironmentRow<PortMapping>;
	type PortPage = EnvironmentPage<PortsPaginatedResponse>;

	let {
		ports,
		selectedIds = $bindable(),
		requestOptions = $bindable(),
		onRetry
	}: {
		ports: PortPage;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
		onRetry: () => Promise<unknown>;
	} = $props();

	let mobileFieldVisibility = $state<Record<string, boolean>>({});

	function formatHostPort(port: PortMapping): string {
		return port.isPublished && port.hostPort ? String(port.hostPort) : m.ports_no_host_binding();
	}

	function formatHostIp(port: PortMapping): string {
		if (!port.isPublished) {
			return m.ports_no_host_binding();
		}
		return port.hostIp?.trim() || '0.0.0.0';
	}

	function mappingTitle(port: PortMapping): string {
		if (port.isPublished && port.hostPort) {
			return `${port.hostPort} -> ${port.containerPort}`;
		}
		return `${port.containerPort}/${port.protocol}`;
	}

	const columns = $derived([
		...environmentColumns<PortRow>(),
		{ accessorKey: 'hostPort', title: m.ports_host_port(), sortable: true, cell: HostPortCell },
		{ accessorKey: 'containerPort', title: m.ports_container_port(), sortable: true, cell: ContainerPortCell },
		{ accessorKey: 'protocol', title: m.common_type(), sortable: true, cell: ProtocolCell },
		{ accessorKey: 'containerName', title: m.container(), sortable: true, cell: ContainerCell },
		{ accessorKey: 'hostIp', title: m.ports_host_ip(), sortable: true, cell: HostIpCell },
		{ accessorKey: 'isPublished', title: m.common_status(), sortable: true, cell: PublishedCell }
	] satisfies ColumnSpec<PortRow>[]);

	const mobileFields = [
		{ id: 'hostPort', label: m.ports_host_port(), defaultVisible: true },
		{ id: 'containerPort', label: m.ports_container_port(), defaultVisible: true },
		{ id: 'protocol', label: m.common_type(), defaultVisible: true },
		{ id: 'containerName', label: m.container(), defaultVisible: true },
		{ id: 'hostIp', label: m.ports_host_ip(), defaultVisible: true },
		{ id: 'isPublished', label: m.common_status(), defaultVisible: true }
	];
</script>

{#snippet HostPortCell({ item }: { item: PortRow })}
	<span class={['font-mono text-sm', { 'text-muted-foreground': !item.isPublished }]}>
		{formatHostPort(item)}
	</span>
{/snippet}

{#snippet ContainerPortCell({ item }: { item: PortRow })}
	<span class="font-mono text-sm">{item.containerPort}</span>
{/snippet}

{#snippet ProtocolCell({ item }: { item: PortRow })}
	<Badge variant="gray">{item.protocol.toUpperCase()}</Badge>
{/snippet}

{#snippet ContainerCell({ item }: { item: PortRow })}
	<a
		class="font-medium hover:underline"
		href={resolve(`/(app)/containers/[containerId]?env=${encodeURIComponent(item.environmentId)}`, {
			containerId: item.containerId
		})}
	>
		{item.containerName}
	</a>
{/snippet}

{#snippet HostIpCell({ item }: { item: PortRow })}
	<span class={['font-mono text-sm', { 'text-muted-foreground': !item.isPublished }]}>
		{formatHostIp(item)}
	</span>
{/snippet}

{#snippet PublishedCell({ item }: { item: PortRow })}
	<Badge variant={item.isPublished ? 'sky' : 'gray'}>{item.isPublished ? m.published() : m.ports_exposed_label()}</Badge>
{/snippet}

{#snippet PortMobileCardSnippet({ item, mobileFieldVisibility }: { item: PortRow; mobileFieldVisibility: MobileFieldVisibility })}
	<UniversalMobileCard
		{item}
		icon={(item: PortMapping) => ({
			component: item.isPublished ? ConnectionIcon : HashIcon,
			variant: item.isPublished ? 'blue' : 'gray'
		})}
		title={(item: PortMapping) => mappingTitle(item)}
		subtitle={(item: PortMapping) => ((mobileFieldVisibility['containerName'] ?? true) ? item.containerName : null)}
		badges={[
			(item: PortMapping) =>
				(mobileFieldVisibility['isPublished'] ?? true)
					? {
							variant: item.isPublished ? 'blue' : 'gray',
							text: String(item.isPublished ? m.published() : m.ports_exposed_label())
						}
					: null
		]}
		fields={[
			{
				label: m.ports_container_port(),
				getValue: (item: PortMapping) => String(item.containerPort),
				icon: HashIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['containerPort'] ?? true
			},
			{
				label: m.common_type(),
				getValue: (item: PortMapping) => item.protocol.toUpperCase(),
				icon: ConnectionIcon,
				iconVariant: 'gray' as const,
				type: 'badge' as const,
				badgeVariant: 'gray' as const,
				show: mobileFieldVisibility['protocol'] ?? true
			},
			{
				label: m.ports_host_ip(),
				getValue: (item: PortMapping) => formatHostIp(item),
				icon: GlobeIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['hostIp'] ?? true
			},
			{
				label: m.container(),
				getValue: (item: PortMapping) => item.containerName,
				icon: ContainersIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['containerName'] ?? true
			}
		]}
		onclick={() =>
			void goto(
				resolve(`/(app)/containers/[containerId]?env=${encodeURIComponent(item.environmentId)}`, {
					containerId: item.containerId
				})
			)}
	/>
{/snippet}

<ArcaneTable
	persistKey="arcane-ports-table"
	items={ports}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	{onRetry}
	{columns}
	{mobileFields}
	mobileCard={PortMobileCardSnippet}
/>
