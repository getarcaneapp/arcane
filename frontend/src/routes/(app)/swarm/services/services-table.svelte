<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import type { ColumnSpec, MobileFieldVisibility } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table';
	import { DockIcon, LayersIcon, GlobeIcon, EllipsisIcon, EditIcon, TrashIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { swarmService } from '$lib/services/swarm-service';
	import type { SwarmServiceSummary, SwarmServicePort } from '$lib/types/swarm.type';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { toast } from 'svelte-sonner';
	import { tryCatch } from '$lib/utils/try-catch';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { truncateImageDigest } from '$lib/utils/string.utils';
	import ServiceEditorDialog from './service-editor-dialog.svelte';

	let {
		services = $bindable(),
		requestOptions = $bindable()
	}: {
		services: Paginated<SwarmServiceSummary>;
		requestOptions: SearchPaginationSortRequest;
	} = $props();

	function formatPorts(ports?: SwarmServicePort[]) {
		if (!ports || ports.length === 0) return m.common_na();
		return ports
			.map((port) => {
				const protocol = port.protocol || 'tcp';
				if (port.publishedPort) {
					return `${port.publishedPort}:${port.targetPort}/${protocol}`;
				}
				return `${port.targetPort}/${protocol}`;
			})
			.join(', ');
	}

	function modeVariant(mode: string): 'green' | 'blue' | 'amber' | 'gray' {
		if (mode === 'replicated') return 'blue';
		if (mode === 'global') return 'green';
		return 'gray';
	}

	let isLoading = $state({ inspect: false, update: false, remove: false });
	let editOpen = $state(false);
	let editServiceId = $state<string | null>(null);
	let editServiceName = $state('');
	let editSpec = $state('');
	let editOptions = $state('');
	let editVersion = $state(0);

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

	const columns = [
		{ accessorKey: 'id', title: m.common_id(), hidden: true },
		{ accessorKey: 'name', title: m.common_name(), sortable: true },
		{ accessorKey: 'image', title: m.common_image(), sortable: true, cell: ImageCell },
		{ accessorKey: 'mode', title: m.swarm_mode(), sortable: true, cell: ModeCell },
		{ accessorKey: 'replicas', title: m.swarm_replicas(), sortable: true },
		{ accessorKey: 'stackName', title: m.swarm_stack(), sortable: true, cell: StackCell },
		{ accessorKey: 'ports', title: m.common_ports(), cell: PortsCell }
	] satisfies ColumnSpec<SwarmServiceSummary>[];

	const mobileFields = [
		{ id: 'image', label: m.common_image(), defaultVisible: true },
		{ id: 'mode', label: m.swarm_mode(), defaultVisible: true },
		{ id: 'replicas', label: m.swarm_replicas(), defaultVisible: true },
		{ id: 'stackName', label: m.swarm_stack(), defaultVisible: false },
		{ id: 'ports', label: m.common_ports(), defaultVisible: false }
	];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet ImageCell({ value }: { value: unknown })}
	<span class="font-mono text-sm">{truncateImageDigest(String(value ?? ''))}</span>
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

{#snippet PortsCell({ value }: { value: unknown })}
	<span class="text-sm">{formatPorts(value as SwarmServicePort[] | undefined)}</span>
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
		title={(item: SwarmServiceSummary) => item.name}
		subtitle={(item: SwarmServiceSummary) => ((mobileFieldVisibility.image ?? true) ? truncateImageDigest(item.image) : null)}
		badges={[
			(item: SwarmServiceSummary) =>
				(mobileFieldVisibility.mode ?? true) ? { variant: modeVariant(item.mode), text: item.mode } : null
		]}
		fields={[
			{
				label: m.swarm_replicas(),
				getValue: (item: SwarmServiceSummary) => String(item.replicas),
				icon: GlobeIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.replicas ?? true
			},
			{
				label: m.swarm_stack(),
				getValue: (item: SwarmServiceSummary) => item.stackName ?? m.common_na(),
				icon: LayersIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.stackName ?? false
			},
			{
				label: m.common_ports(),
				getValue: (item: SwarmServiceSummary) => formatPorts(item.ports),
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
				<DropdownMenu.Item onclick={() => openEdit(item)} disabled={isAnyLoading}>
					<EditIcon class="size-4" />
					{m.common_edit()}
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
/>
