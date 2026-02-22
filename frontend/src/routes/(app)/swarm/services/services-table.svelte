<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import type { ColumnSpec, MobileFieldVisibility } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table';
	import {
		DockIcon,
		LayersIcon,
		GlobeIcon,
		EllipsisIcon,
		EditIcon,
		TrashIcon,
		NetworksIcon,
		VolumesIcon,
		FileTextIcon,
		JobsIcon,
		RedeployIcon
	} from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { swarmService } from '$lib/services/swarm-service';
	import type { SwarmServiceSummary, SwarmServicePort, SwarmServiceMount } from '$lib/types/swarm.type';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { toast } from 'svelte-sonner';
	import { tryCatch } from '$lib/utils/try-catch';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { truncateImageDigest } from '$lib/utils/string.utils';
	import { goto } from '$app/navigation';
	import ServiceEditorDialog from './service-editor-dialog.svelte';
	import ServiceLogsDialog from './service-logs-dialog.svelte';

	let {
		services = $bindable(),
		requestOptions = $bindable()
	}: {
		services: Paginated<SwarmServiceSummary>;
		requestOptions: SearchPaginationSortRequest;
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

	function modeVariant(mode: string): 'green' | 'blue' | 'amber' | 'gray' {
		if (mode === 'replicated') return 'blue';
		if (mode === 'global') return 'green';
		return 'gray';
	}

	let isLoading = $state({ inspect: false, update: false, remove: false, rollback: false });
	let editOpen = $state(false);
	let editServiceId = $state<string | null>(null);
	let editServiceName = $state('');
	let editSpec = $state('');
	let editOptions = $state('');
	let editVersion = $state(0);

	let logsOpen = $state(false);
	let logsServiceId = $state<string | null>(null);
	let logsServiceName = $state('');

	const isAnyLoading = $derived(Object.values(isLoading).some(Boolean));

	async function openEdit(service: SwarmServiceSummary) {
		if (!service?.id) return;
		isLoading.inspect = true;
		const result = await tryCatch(swarmService.getService(service.id));
		isLoading.inspect = false;
		if (result.error) {
			toast.error(m.common_update_failed({ resource: `${m.swarm_service()} "${service.name}"` }));
			return;
		}

		editServiceId = service.id;
		editServiceName = service.name;
		editVersion = (result.data as any)?.version?.index ?? (result.data as any)?.version?.Index ?? 0;
		editSpec = JSON.stringify((result.data as any)?.spec ?? {}, null, 2);
		editOptions = '';
		editOpen = true;
	}

	async function handleUpdate(payload: { spec: Record<string, unknown>; options?: Record<string, unknown> }) {
		if (!editServiceId) return;
		handleApiResultWithCallbacks({
			result: await tryCatch(swarmService.updateService(editServiceId, { version: editVersion, ...payload })),
			message: m.common_update_failed({ resource: `${m.swarm_service()} "${editServiceName}"` }),
			setLoadingState: (v) => (isLoading.update = v),
			onSuccess: async () => {
				toast.success(m.common_update_success({ resource: `${m.swarm_service()} "${editServiceName}"` }));
				services = await swarmService.getServices(requestOptions);
				editOpen = false;
			}
		});
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
							services = await swarmService.getServices(requestOptions);
						}
					});
				}
			}
		});
	}

	function handleRollback(service: SwarmServiceSummary) {
		openConfirmDialog({
			title: m.swarm_service_rollback_title(),
			message: m.swarm_service_rollback_confirm({ name: service.name }),
			confirm: {
				label: m.swarm_service_rollback(),
				destructive: false,
				action: async () => {
					handleApiResultWithCallbacks({
						result: await tryCatch(swarmService.rollbackService(service.id)),
						message: m.swarm_service_rollback_failed({ name: service.name }),
						setLoadingState: (v) => (isLoading.rollback = v),
						onSuccess: async () => {
							toast.success(m.swarm_service_rollback_success({ name: service.name }));
							services = await swarmService.getServices(requestOptions);
						}
					});
				}
			}
		});
	}

	function viewTasks(service: SwarmServiceSummary) {
		goto(`/swarm/tasks?search=${encodeURIComponent(service.name)}`);
	}

	function openLogs(service: SwarmServiceSummary) {
		logsServiceId = service.id;
		logsServiceName = service.name;
		logsOpen = true;
	}

	const columns = [
		{ accessorKey: 'id', title: m.common_id(), hidden: true },
		{ accessorKey: 'stackName', title: m.swarm_stack(), sortable: true, cell: StackCell },
		{ accessorKey: 'name', title: m.swarm_service(), sortable: true, cell: NameCell },
		{ accessorKey: 'mode', title: m.swarm_mode(), sortable: true, cell: ModeCell },
		{ accessorKey: 'replicas', title: m.swarm_replicas(), sortable: true, cell: ReplicasCell },
		{ id: 'nodes', accessorFn: (item: SwarmServiceSummary) => item.nodes, title: m.swarm_nodes_column(), cell: NodesCell },
		{ id: 'networks', accessorFn: (item: SwarmServiceSummary) => item.networks, title: m.swarm_networks(), cell: NetworksCell },
		{ accessorKey: 'ports', title: m.common_ports(), cell: PortsCell }
	] satisfies ColumnSpec<SwarmServiceSummary>[];

	const mobileFields = [
		{ id: 'stackName', label: m.swarm_stack(), defaultVisible: true },
		{ id: 'mode', label: m.swarm_mode(), defaultVisible: true },
		{ id: 'replicas', label: m.swarm_replicas(), defaultVisible: true },
		{ id: 'nodes', label: m.swarm_nodes_column(), defaultVisible: true },
		{ id: 'networks', label: m.swarm_networks(), defaultVisible: false },
		{ id: 'ports', label: m.common_ports(), defaultVisible: false }
	];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet NameCell({ item }: { item: SwarmServiceSummary })}
	<span class="text-sm font-medium">{getShortName(item.name, item.stackName)}</span>
{/snippet}

{#snippet ModeCell({ value }: { value: unknown })}
	<StatusBadge text={String(value ?? m.common_unknown())} variant={modeVariant(String(value ?? ''))} />
{/snippet}

{#snippet StackCell({ value }: { value: unknown })}
	{#if value}
		<span class="text-sm">{String(value)}</span>
	{:else}
		<span class="text-muted-foreground text-sm">{m.common_na()}</span>
	{/if}
{/snippet}

{#snippet ReplicasCell({ item }: { item: SwarmServiceSummary })}
	<span class="font-mono text-sm">{item.runningReplicas} / {item.replicas}</span>
{/snippet}

{#snippet OverflowCell({ items }: { items: string[] })}
	{#if !items || items.length === 0}
		<span class="text-muted-foreground text-sm">{m.common_na()}</span>
	{:else}
		<div class="flex flex-col gap-0.5">
			{#each items.slice(0, MAX_OVERFLOW_ITEMS) as item}
				<span class="max-w-45 truncate font-mono text-sm">{item}</span>
			{/each}
			{#if items.length > MAX_OVERFLOW_ITEMS}
				<span class="text-muted-foreground text-xs font-medium">
					{m.swarm_n_more({ count: items.length - MAX_OVERFLOW_ITEMS })}
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

{#snippet ExpandedRowDetail({ item }: { item: SwarmServiceSummary })}
	<div class="grid grid-cols-1 gap-4 md:grid-cols-3">
		<!-- Full Service Name + Image -->
		<div class="space-y-3">
			<div>
				<h4 class="text-muted-foreground mb-1 text-xs font-medium tracking-wider uppercase">
					{m.swarm_full_name()}
				</h4>
				<span class="text-sm font-medium">{item.name}</span>
			</div>
			<div>
				<h4 class="text-muted-foreground mb-1 text-xs font-medium tracking-wider uppercase">
					{m.common_image()}
				</h4>
				<span class="font-mono text-sm break-all">{truncateImageDigest(item.image)}</span>
			</div>
		</div>

		<!-- Mounts -->
		<div>
			<h4 class="text-muted-foreground mb-1 text-xs font-medium tracking-wider uppercase">
				{m.common_mounts()}
			</h4>
			{#if !item.mounts || item.mounts.length === 0}
				<span class="text-muted-foreground text-sm">{m.common_na()}</span>
			{:else}
				<div class="flex flex-col gap-1">
					{#each item.mounts as mount}
						<div class="flex items-center gap-2 text-sm">
							<StatusBadge text={mount.type} variant="gray" size="sm" minWidth="none" />
							<span class="max-w-62.5 truncate font-mono" title="{mount.source || ''} → {mount.target}">
								{mount.source || '(anon)'} → {mount.target}
							</span>
							{#if mount.readOnly}
								<StatusBadge text="ro" variant="amber" size="sm" minWidth="none" />
							{/if}
						</div>
					{/each}
				</div>
			{/if}
		</div>

		<!-- All Nodes -->
		<div>
			<h4 class="text-muted-foreground mb-1 text-xs font-medium tracking-wider uppercase">
				{m.swarm_nodes_column()} ({item.nodes?.length ?? 0})
			</h4>
			{#if !item.nodes || item.nodes.length === 0}
				<span class="text-muted-foreground text-sm">{m.common_na()}</span>
			{:else}
				<div class="flex flex-wrap gap-1">
					{#each item.nodes as node}
						<StatusBadge text={node} variant="gray" size="sm" minWidth="none" />
					{/each}
				</div>
			{/if}
		</div>

		<!-- Full Ports (only if overflowed) -->
		{#if item.ports && item.ports.length > MAX_OVERFLOW_ITEMS}
			<div class="md:col-span-3">
				<h4 class="text-muted-foreground mb-1 text-xs font-medium tracking-wider uppercase">
					{m.common_ports()} ({item.ports.length})
				</h4>
				<div class="flex flex-wrap gap-1">
					{#each item.ports as port}
						<StatusBadge text={formatPort(port)} variant="gray" size="sm" minWidth="none" />
					{/each}
				</div>
			</div>
		{/if}

		<!-- Full Networks (only if overflowed) -->
		{#if item.networks && item.networks.length > MAX_OVERFLOW_ITEMS}
			<div class="md:col-span-3">
				<h4 class="text-muted-foreground mb-1 text-xs font-medium tracking-wider uppercase">
					{m.swarm_networks()} ({item.networks.length})
				</h4>
				<div class="flex flex-wrap gap-1">
					{#each item.networks as network}
						<StatusBadge text={network} variant="gray" size="sm" minWidth="none" />
					{/each}
				</div>
			</div>
		{/if}
	</div>
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
			variant: item.mode === 'global' ? 'emerald' : 'blue'
		})}
		title={(item: SwarmServiceSummary) => getShortName(item.name, item.stackName)}
		subtitle={(item: SwarmServiceSummary) => ((mobileFieldVisibility.stackName ?? true) ? (item.stackName ?? null) : null)}
		badges={[
			(item: SwarmServiceSummary) =>
				(mobileFieldVisibility.mode ?? true) ? { variant: modeVariant(item.mode), text: item.mode } : null
		]}
		fields={[
			{
				label: m.swarm_replicas(),
				getValue: (item: SwarmServiceSummary) => `${item.runningReplicas} / ${item.replicas}`,
				icon: GlobeIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.replicas ?? true
			},
			{
				label: m.swarm_nodes_column(),
				getValue: (item: SwarmServiceSummary) =>
					item.nodes?.length
						? item.nodes.slice(0, 3).join(', ') + (item.nodes.length > 3 ? ` +${item.nodes.length - 3}` : '')
						: m.common_na(),
				icon: NetworksIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.nodes ?? true
			},
			{
				label: m.swarm_networks(),
				getValue: (item: SwarmServiceSummary) => (item.networks?.length ? item.networks.join(', ') : m.common_na()),
				icon: NetworksIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.networks ?? false
			},
			{
				label: m.common_ports(),
				getValue: (item: SwarmServiceSummary) => formatPortsList(item.ports).join(', ') || m.common_na(),
				icon: GlobeIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.ports ?? false
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: SwarmServiceSummary })}
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
				<DropdownMenu.Item onclick={() => openLogs(item)}>
					<FileTextIcon class="size-4" />
					{m.common_logs()}
				</DropdownMenu.Item>
				<DropdownMenu.Item onclick={() => viewTasks(item)}>
					<JobsIcon class="size-4" />
					{m.swarm_service_view_tasks()}
				</DropdownMenu.Item>
				<DropdownMenu.Item onclick={() => openEdit(item)} disabled={isAnyLoading}>
					<EditIcon class="size-4" />
					{m.common_edit()}
				</DropdownMenu.Item>
				<DropdownMenu.Item onclick={() => handleRollback(item)} disabled={isAnyLoading}>
					<RedeployIcon class="size-4" />
					{m.swarm_service_rollback()}
				</DropdownMenu.Item>
				<DropdownMenu.Separator />
				<DropdownMenu.Item variant="destructive" onclick={() => handleDelete(item)} disabled={isAnyLoading}>
					<TrashIcon class="size-4" />
					{m.common_delete()}
				</DropdownMenu.Item>
			</DropdownMenu.Group>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/snippet}

<ServiceEditorDialog
	bind:open={editOpen}
	title={`${m.common_edit()} ${m.swarm_service()}`}
	description={m.common_edit_description()}
	submitLabel={m.common_save()}
	initialSpec={editSpec}
	initialOptions={editOptions}
	isLoading={isLoading.update}
	onSubmit={handleUpdate}
/>

<ServiceLogsDialog bind:open={logsOpen} serviceId={logsServiceId} serviceName={logsServiceName} />

<ArcaneTable
	persistKey="arcane-swarm-services-table"
	items={services}
	bind:requestOptions
	bind:mobileFieldVisibility
	selectionDisabled={true}
	onRefresh={async (options) => (services = await swarmService.getServices(options))}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={ServiceMobileCardSnippet}
	expandedRowContent={ExpandedRowDetail}
/>
