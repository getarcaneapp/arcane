<script lang="ts">
	import type { NetworkSummaryDto, NetworkUsageCounts } from '$lib/types/docker';
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import RowActionsMenu from '$lib/components/arcane-table/row-actions-menu.svelte';
	import { goto } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { Badge } from '$lib/components/ui/badge';
	import { handleApiResultWithCallbacks } from '$lib/utils/api';
	import { tryCatch } from '$lib/utils/api';
	import { DEFAULT_NETWORK_NAMES } from '$lib/constants';
	import InUseStatus from '$lib/components/arcane-table/cells/in-use-status.svelte';
	import type { SearchPaginationSortRequest, Paginated } from '$lib/types/shared';
	import { capitalizeFirstLetter } from '$lib/utils/formatting';
	import type { ColumnSpec, BulkAction } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table';
	import { m } from '$lib/paraglide/messages';
	import { networkService } from '$lib/services/network-service';
	import { NetworksIcon, GlobeIcon, InspectIcon, TrashIcon } from '$lib/icons';
	import { activityToastOptions, extractActivityId } from '$lib/utils/activity-toast';
	import { bulkConfirmAndRun } from '$lib/utils/bulk-actions';

	type FieldVisibility = Record<string, boolean>;

	let {
		networks = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable(),
		onNetworksChange,
		onRefreshData
	}: {
		networks: Paginated<NetworkSummaryDto, NetworkUsageCounts>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
		onNetworksChange?: (networks: Paginated<NetworkSummaryDto, NetworkUsageCounts>) => void;
		onRefreshData?: (options: SearchPaginationSortRequest) => Promise<void>;
	} = $props();

	let isLoading = $state({
		remove: false
	});

	async function refreshNetworks(options: SearchPaginationSortRequest = requestOptions) {
		if (onRefreshData) {
			await onRefreshData(options);
			if (onNetworksChange) {
				onNetworksChange(networks);
			}
			return;
		}
		networks = await networkService.getNetworks(options);
		onNetworksChange?.(networks);
	}

	async function handleDeleteNetwork(id: string, name: string) {
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
					handleApiResultWithCallbacks({
						result: await tryCatch(networkService.deleteNetwork(id)),
						message: m.common_delete_failed({ resource: `${m.resource_network()} "${safeName}"` }),
						setLoadingState: (value) => (isLoading.remove = value),
						onSuccess: async (data) => {
							toast.success(
								m.common_delete_success({ resource: `${m.resource_network()} "${safeName}"` }),
								activityToastOptions(extractActivityId(data))
							);
							await refreshNetworks();
						}
					});
				}
			}
		});
	}

	function handleDeleteSelectedNetworks(ids: string[]) {
		const selectedNetworkList = networks.data.filter((n) => ids.includes(n.id));
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
			run: (id) => networkService.deleteNetwork(id),
			messages: {
				success: (count) => m.common_bulk_delete_success({ count, resource: m.resource_networks_cap() }),
				partial: (success, total, failed) =>
					m.common_bulk_delete_partial({ success, total, failed, resource: m.resource_networks_cap() }),
				failure: () => m.common_bulk_delete_failed({ count: ids.length, resource: m.resource_networks_cap() })
			},
			setLoading: (loading) => (isLoading.remove = loading),
			onComplete: async (result) => {
				if (result.success > 0) await refreshNetworks();
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

	const columns = [
		{ accessorKey: 'id', title: m.common_id(), cell: IdCell, hidden: true },
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'inUse', title: m.common_status(), sortable: true, cell: StatusCell },
		{ accessorKey: 'driver', title: m.common_driver(), sortable: true, cell: DriverCell },
		{ accessorKey: 'scope', title: m.common_scope(), sortable: true, cell: ScopeCell }
	] satisfies ColumnSpec<NetworkSummaryDto>[];

	const mobileFields = [
		{ id: 'id', label: m.common_id(), defaultVisible: false },
		{ id: 'inUse', label: m.common_status(), defaultVisible: true },
		{ id: 'driver', label: m.common_driver(), defaultVisible: true },
		{ id: 'scope', label: m.common_scope(), defaultVisible: true }
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

{#snippet NameCell({ item }: { item: NetworkSummaryDto })}
	<a class="font-medium hover:underline" href="/networks/{item.id}">{item.name}</a>
{/snippet}

{#snippet IdCell({ item }: { item: NetworkSummaryDto })}
	<span class="truncate font-mono text-sm">{String(item.id)}</span>
{/snippet}

{#snippet DriverCell({ item }: { item: NetworkSummaryDto })}
	<Badge
		variant={item.driver === 'bridge'
			? 'blue'
			: item.driver === 'overlay'
				? 'purple'
				: item.driver === 'ipvlan'
					? 'red'
					: item.driver === 'macvlan'
						? 'orange'
						: 'gray'}
		minWidth="20">{capitalizeFirstLetter(item.driver)}</Badge
	>
{/snippet}

{#snippet ScopeCell({ item }: { item: NetworkSummaryDto })}
	<Badge variant={item.scope === 'local' ? 'green' : 'amber'} minWidth="20">{capitalizeFirstLetter(item.scope)}</Badge>
{/snippet}

{#snippet StatusCell({ item }: { item: NetworkSummaryDto })}
	{#if item.isDefault}
		<Badge variant="sky" minWidth="20">{m.networks_predefined()}</Badge>
	{:else}
		<InUseStatus inUse={item.inUse} />
	{/if}
{/snippet}

{#snippet NetworkMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: NetworkSummaryDto;
	mobileFieldVisibility: FieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={(item: NetworkSummaryDto) => ({
			component: NetworksIcon,
			variant: item.inUse ? 'emerald' : 'amber'
		})}
		title={(item: NetworkSummaryDto) => item.name}
		subtitle={(item: NetworkSummaryDto) => ((mobileFieldVisibility['id'] ?? true) ? item.id : null)}
		badges={[
			(item: NetworkSummaryDto) =>
				(mobileFieldVisibility['inUse'] ?? true)
					? (item.isDefault ?? false) || DEFAULT_NETWORK_NAMES.has(item.name)
						? { variant: 'gray', text: m.networks_predefined() }
						: item.inUse
							? { variant: 'green', text: m.common_in_use() }
							: { variant: 'amber', text: m.common_unused() }
					: null
		]}
		fields={[
			{
				label: m.common_driver(),
				getValue: (item: NetworkSummaryDto) => capitalizeFirstLetter(item.driver),
				icon: NetworksIcon,
				iconVariant: 'gray' as const,
				type: 'badge' as const,
				badgeVariant: getDriverVariant(item.driver),
				show: mobileFieldVisibility['driver'] ?? true
			},
			{
				label: m.common_scope(),
				getValue: (item: NetworkSummaryDto) => capitalizeFirstLetter(item.scope),
				icon: GlobeIcon,
				iconVariant: 'gray' as const,
				type: 'badge' as const,
				badgeVariant: item.scope === 'local' ? ('green' as const) : ('amber' as const),
				show: mobileFieldVisibility['scope'] ?? true
			}
		]}
		rowActions={RowActions}
		onclick={() => goto(`/networks/${item.id}`)}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: NetworkSummaryDto })}
	<RowActionsMenu>
		<DropdownMenu.Item onclick={() => goto(`/networks/${item.id}`)} disabled={isAnyLoading}>
			<InspectIcon class="size-4" />
			{m.common_inspect()}
		</DropdownMenu.Item>

		<DropdownMenu.Separator />

		<DropdownMenu.Item
			variant="destructive"
			onclick={() => handleDeleteNetwork(item.id, item.name)}
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
	onRefresh={async (options) => {
		requestOptions = options;
		await refreshNetworks(options);
		return networks;
	}}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={NetworkMobileCardSnippet}
/>
