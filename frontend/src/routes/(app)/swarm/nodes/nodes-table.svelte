<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import type { ColumnSpec, MobileFieldVisibility } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table';
	import { UsersIcon, EnvironmentsIcon, EllipsisIcon, InspectIcon, TrashIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { swarmService } from '$lib/services/swarm-service';
	import type { SwarmNodeSummary } from '$lib/types/swarm.type';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { capitalizeFirstLetter } from '$lib/utils/string.utils';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { toast } from 'svelte-sonner';
	import { tryCatch } from '$lib/utils/try-catch';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { goto } from '$app/navigation';
	import userStore from '$lib/stores/user-store';
	import { fromStore } from 'svelte/store';

	let {
		nodes = $bindable(),
		requestOptions = $bindable()
	}: {
		nodes: Paginated<SwarmNodeSummary>;
		requestOptions: SearchPaginationSortRequest;
	} = $props();

	const storeUser = fromStore(userStore);
	const isAdmin = $derived(!!storeUser.current?.roles?.includes('admin'));
	let isLoading = $state(false);

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

	async function refreshNodes() {
		nodes = await swarmService.getNodes(requestOptions);
	}

	function inspectNodeTasks(node: SwarmNodeSummary) {
		goto(`/swarm/tasks?nodeId=${encodeURIComponent(node.id)}&search=${encodeURIComponent(node.hostname)}`);
	}

	async function mutateNode(action: () => Promise<void>, successMessage: string, failureMessage: string) {
		handleApiResultWithCallbacks({
			result: await tryCatch(action()),
			message: failureMessage,
			setLoadingState: (v) => (isLoading = v),
			onSuccess: async () => {
				toast.success(successMessage);
				await refreshNodes();
			}
		});
	}

	function setAvailability(node: SwarmNodeSummary, availability: 'active' | 'pause' | 'drain') {
		mutateNode(
			() => swarmService.updateNode(node.id, { availability }),
			m.swarm_node_availability_update_success({ name: node.hostname, availability }),
			m.swarm_node_update_failed({ name: node.hostname })
		);
	}

	function promoteNode(node: SwarmNodeSummary) {
		mutateNode(
			() => swarmService.promoteNode(node.id),
			m.swarm_node_promote_success({ name: node.hostname }),
			m.swarm_node_promote_failed({ name: node.hostname })
		);
	}

	function demoteNode(node: SwarmNodeSummary) {
		mutateNode(
			() => swarmService.demoteNode(node.id),
			m.swarm_node_demote_success({ name: node.hostname }),
			m.swarm_node_demote_failed({ name: node.hostname })
		);
	}

	function removeNode(node: SwarmNodeSummary) {
		openConfirmDialog({
			title: m.common_delete_title({ resource: m.swarm_node() }),
			message: m.common_delete_confirm({ resource: m.swarm_node() }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					mutateNode(
						() => swarmService.removeNode(node.id, true),
						m.swarm_node_remove_success({ name: node.hostname }),
						m.swarm_node_remove_failed({ name: node.hostname })
					);
				}
			}
		});
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
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: SwarmNodeSummary })}
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
				<DropdownMenu.Item onclick={() => inspectNodeTasks(item)}>
					<InspectIcon class="size-4" />
					{m.common_inspect()}
				</DropdownMenu.Item>
				<DropdownMenu.Separator />
				<DropdownMenu.Item onclick={() => promoteNode(item)} disabled={!isAdmin || isLoading || item.role === 'manager'}>
					{m.swarm_node_promote()}
				</DropdownMenu.Item>
				<DropdownMenu.Item onclick={() => demoteNode(item)} disabled={!isAdmin || isLoading || item.role !== 'manager'}>
					{m.swarm_node_demote()}
				</DropdownMenu.Item>
				<DropdownMenu.Item
					onclick={() => setAvailability(item, 'drain')}
					disabled={!isAdmin || isLoading || item.availability === 'drain'}
				>
					{m.swarm_node_drain()}
				</DropdownMenu.Item>
				<DropdownMenu.Item
					onclick={() => setAvailability(item, 'active')}
					disabled={!isAdmin || isLoading || item.availability === 'active'}
				>
					{m.swarm_node_activate()}
				</DropdownMenu.Item>
				<DropdownMenu.Separator />
				<DropdownMenu.Item variant="destructive" onclick={() => removeNode(item)} disabled={!isAdmin || isLoading}>
					<TrashIcon class="size-4" />
					{m.common_delete()}
				</DropdownMenu.Item>
			</DropdownMenu.Group>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
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
	rowActions={RowActions}
	mobileCard={NodeMobileCardSnippet}
/>
