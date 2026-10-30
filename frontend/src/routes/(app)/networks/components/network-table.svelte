<script module lang="ts">
	import { resolve } from '$app/paths';

	import type { NetworkSummaryDto } from '#lib/types/docker.js';

	function ipamValues(item: NetworkSummaryDto, key: 'subnet' | 'gateway'): string[] {
		return (item.ipam?.config ?? []).map((c) => c[key]).filter((v): v is string => !!v);
	}
</script>

<script lang="ts">
	import { goto } from '$app/navigation';
	import { useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import InUseStatus from '#lib/components/arcane-table/cells/in-use-status.svelte';
	import { environmentColumns } from '#lib/components/arcane-table/environment-column.js';
	import type { ColumnSpec, BulkAction } from '#lib/components/arcane-table/index.js';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { DEFAULT_NETWORK_NAMES } from '#lib/constants.js';
	import { NetworksIcon, GlobeIcon, InspectIcon, TrashIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { networkService, type NetworksPaginatedResponse } from '#lib/services/network-service.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { bulkConfirmAndRun } from '#lib/utils/bulk-actions.js';
	import { type EnvironmentPage, type EnvironmentRow, rowKey } from '#lib/utils/environments.js';
	import { capitalizeFirstLetter } from '#lib/utils/formatting.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	type FieldVisibility = Record<string, boolean>;
	type NetworkRow = EnvironmentRow<NetworkSummaryDto>;
	type NetworkPage = EnvironmentPage<NetworksPaginatedResponse>;

	let {
		networks,
		selectedIds = $bindable(),
		requestOptions = $bindable(),
		onRetry
	}: {
		networks: NetworkPage;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
		onRetry: () => Promise<unknown>;
	} = $props();

	const queryClient = useQueryClient();

	const isLoading = $state({
		remove: false
	});

	async function handleDeleteNetwork(id: string, name: string, environmentId: string) {
		const safeName = name?.trim() || m.common_unknown();
		if (DEFAULT_NETWORK_NAMES.has(name)) {
			toast.error(m.networks_cannot_delete_default({ name: safeName }));
			return;
		}
		openConfirmDialog({
			title: m.common_delete_title({ resource: m.resource_network() }),
			message: m.networks_delete_confirm_message({ name: safeName }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					isLoading.remove = true;
					await handleApiResultWithCallbacks({
						result: await tryCatch(networkService.deleteNetwork(id, environmentId)),
						message: m.common_delete_failed({ resource: `${m.resource_network()} "${safeName}"` }),
						setLoadingState: (value) => (isLoading.remove = value),
						onSuccess: async (data) => {
							toast.success(
								m.common_delete_success({ resource: `${m.resource_network()} "${safeName}"` }),
								activityToastOptions(extractActivityId(data))
							);
							await queryClient.invalidateQueries({ queryKey: queryKeys.networks.listPrefix(environmentStore.shownKey) });
						}
					});
				}
			}
		});
	}

	function handleDeleteSelectedNetworks(ids: string[]) {
		const selectedNetworkList = networks.data.filter((n) => ids.includes(rowKey(n)));
		const defaultNetworks = selectedNetworkList.filter((n) => n.isDefault);

		if (defaultNetworks.length > 0) {
			const names = defaultNetworks.map((n) => n.name ?? m.common_unknown()).join(', ');
			toast.error(m.networks_cannot_delete_default_many({ names }));
			return;
		}

		bulkConfirmAndRun({
			ids,
			title: m.networks_delete_selected_title({ count: ids.length }),
			message: m.networks_delete_selected_message({ count: ids.length }),
			confirmLabel: m.common_delete(),
			destructive: true,
			runInEnvironment: (id, environmentId) => networkService.deleteNetwork(id, environmentId),
			messages: {
				success: (count) => m.common_bulk_delete_success({ count, resource: m.resource_networks_cap() }),
				partial: (success, total, failed) =>
					m.common_bulk_delete_partial({ success, total, failed, resource: m.resource_networks_cap() }),
				failure: () => m.common_bulk_delete_failed({ count: ids.length, resource: m.resource_networks_cap() })
			},
			setLoading: (loading) => (isLoading.remove = loading),
			onComplete: async (result) => {
				if (result.success > 0)
					await queryClient.invalidateQueries({ queryKey: queryKeys.networks.listPrefix(environmentStore.shownKey) });
			},
			clearSelection: () => (selectedIds = [])
		});
	}

	const isAnyLoading = $derived(Object.values(isLoading).some((l) => l));

	function getDriverVariant(driver: string): 'blue' | 'purple' | 'red' | 'orange' | 'gray' {
		const variantMap: Record<string, 'blue' | 'purple' | 'red' | 'orange' | 'gray'> = {
			bridge: 'blue',
			overlay: 'purple',
			ipvlan: 'red',
			macvlan: 'orange'
		};
		return variantMap[driver] || 'gray';
	}

	function getNetworkStatusBadge(
		item: NetworkRow,
		visible: boolean
	): { variant: 'gray' | 'green' | 'amber'; text: string } | null {
		if (!visible) return null;
		if ((item.isDefault ?? false) || DEFAULT_NETWORK_NAMES.has(item.name)) {
			return { variant: 'gray', text: m.networks_predefined() };
		}
		if (item.inUse) return { variant: 'green', text: m.common_in_use() };
		return { variant: 'amber', text: m.common_unused() };
	}

	const columns = $derived([
		...environmentColumns<NetworkRow>(),
		{ accessorKey: 'id', title: m.common_id(), cell: IdCell, hidden: true },
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'inUse', title: m.common_status(), sortable: true, cell: StatusCell },
		{ accessorKey: 'driver', title: m.common_driver(), sortable: true, cell: DriverCell },
		{ accessorKey: 'scope', title: m.common_scope(), sortable: true, cell: ScopeCell },
		{
			id: 'subnet',
			accessorFn: (row) => ipamValues(row, 'subnet').join(', '),
			title: m.common_subnet(),
			sortable: true,
			cell: SubnetCell
		},
		{
			id: 'gateway',
			accessorFn: (row) => ipamValues(row, 'gateway').join(', '),
			title: m.common_gateway(),
			sortable: true,
			cell: GatewayCell
		}
	] satisfies ColumnSpec<NetworkRow>[]);

	const mobileFields = [
		{ id: 'id', label: m.common_id(), defaultVisible: false },
		{ id: 'inUse', label: m.common_status(), defaultVisible: true },
		{ id: 'driver', label: m.common_driver(), defaultVisible: true },
		{ id: 'scope', label: m.common_scope(), defaultVisible: true },
		{ id: 'subnet', label: m.common_subnet(), defaultVisible: true },
		{ id: 'gateway', label: m.common_gateway(), defaultVisible: true }
	];

	const bulkActions = $derived.by<BulkAction[]>(() => [
		{
			id: 'remove',
			label: m.common_remove_selected_count({ count: selectedIds?.length ?? 0 }),
			action: 'remove',
			onClick: handleDeleteSelectedNetworks,
			loading: isLoading.remove,
			disabled: isLoading.remove,
			icon: TrashIcon
		}
	]);

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet NameCell({ item }: { item: NetworkRow })}
	<a
		class="font-medium hover:underline"
		href={resolve(`/(app)/networks/[networkId]?env=${encodeURIComponent(item.environmentId)}`, { networkId: item.id })}
		>{item.name}</a
	>
{/snippet}

{#snippet IdCell({ item }: { item: NetworkRow })}
	<span class="truncate font-mono text-sm">{String(item.id)}</span>
{/snippet}

{#snippet DriverCell({ item }: { item: NetworkRow })}
	<Badge variant={getDriverVariant(item.driver)} minWidth="20">{capitalizeFirstLetter(item.driver)}</Badge>
{/snippet}

{#snippet ScopeCell({ item }: { item: NetworkRow })}
	<Badge variant={item.scope === 'local' ? 'green' : 'amber'} minWidth="20">{capitalizeFirstLetter(item.scope)}</Badge>
{/snippet}

{#snippet SubnetCell({ item }: { item: NetworkRow })}
	{@const subnets = ipamValues(item, 'subnet')}
	{#if subnets.length === 0}
		<span class="text-muted-foreground">—</span>
	{:else}
		<div class="flex flex-col font-mono text-sm">
			{#each subnets as subnet (subnet)}
				<span>{subnet}</span>
			{/each}
		</div>
	{/if}
{/snippet}

{#snippet GatewayCell({ item }: { item: NetworkRow })}
	{@const gateways = ipamValues(item, 'gateway')}
	{#if gateways.length === 0}
		<span class="text-muted-foreground">—</span>
	{:else}
		<div class="flex flex-col font-mono text-sm">
			{#each gateways as gateway (gateway)}
				<span>{gateway}</span>
			{/each}
		</div>
	{/if}
{/snippet}

{#snippet StatusCell({ item }: { item: NetworkRow })}
	{#if item.isDefault}
		<Badge variant="sky" minWidth="20">{m.networks_predefined()}</Badge>
	{:else}
		<InUseStatus inUse={item.inUse} />
	{/if}
{/snippet}

{#snippet NetworkMobileCardSnippet({ item, mobileFieldVisibility }: { item: NetworkRow; mobileFieldVisibility: FieldVisibility })}
	<UniversalMobileCard
		{item}
		icon={(item: NetworkRow) => ({
			component: NetworksIcon,
			variant: item.inUse ? 'emerald' : 'amber'
		})}
		title={(item: NetworkRow) => item.name}
		subtitle={(item: NetworkRow) => ((mobileFieldVisibility['id'] ?? true) ? item.id : null)}
		badges={[(item: NetworkRow) => getNetworkStatusBadge(item, mobileFieldVisibility['inUse'] ?? true)]}
		fields={[
			{
				label: m.common_driver(),
				getValue: (item: NetworkRow) => capitalizeFirstLetter(item.driver),
				icon: NetworksIcon,
				iconVariant: 'gray' as const,
				type: 'badge' as const,
				badgeVariant: getDriverVariant(item.driver),
				show: mobileFieldVisibility['driver'] ?? true
			},
			{
				label: m.common_scope(),
				getValue: (item: NetworkRow) => capitalizeFirstLetter(item.scope),
				icon: GlobeIcon,
				iconVariant: 'gray' as const,
				type: 'badge' as const,
				badgeVariant: item.scope === 'local' ? ('green' as const) : ('amber' as const),
				show: mobileFieldVisibility['scope'] ?? true
			},
			{
				label: m.common_subnet(),
				getValue: (item: NetworkRow) => ipamValues(item, 'subnet').join(', ') || '—',
				icon: NetworksIcon,
				iconVariant: 'gray' as const,
				type: 'mono' as const,
				show: mobileFieldVisibility['subnet'] ?? true
			},
			{
				label: m.common_gateway(),
				getValue: (item: NetworkRow) => ipamValues(item, 'gateway').join(', ') || '—',
				icon: GlobeIcon,
				iconVariant: 'gray' as const,
				type: 'mono' as const,
				show: mobileFieldVisibility['gateway'] ?? true
			}
		]}
		rowActions={RowActions}
		onclick={() =>
			goto(resolve(`/(app)/networks/[networkId]?env=${encodeURIComponent(item.environmentId)}`, { networkId: item.id }))}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: NetworkRow })}
	<RowActionsMenu>
		<DropdownMenu.Item
			onclick={() =>
				goto(resolve(`/(app)/networks/[networkId]?env=${encodeURIComponent(item.environmentId)}`, { networkId: item.id }))}
			disabled={isAnyLoading}
		>
			<InspectIcon class="size-4" />
			{m.common_inspect()}
		</DropdownMenu.Item>

		<DropdownMenu.Separator />

		<DropdownMenu.Item
			variant="destructive"
			onclick={() => handleDeleteNetwork(item.id, item.name, item.environmentId)}
			disabled={isAnyLoading || item.isDefault || DEFAULT_NETWORK_NAMES.has(item.name)}
		>
			<TrashIcon class="size-4" />
			{m.common_delete()}
		</DropdownMenu.Item>
	</RowActionsMenu>
{/snippet}

<ArcaneTable
	persistKey="arcane-networks-table"
	items={networks}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	{bulkActions}
	{onRetry}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={NetworkMobileCardSnippet}
/>
