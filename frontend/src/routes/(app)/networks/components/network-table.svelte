<script module lang="ts">
	import type { NetworkSummaryDto } from '#lib/types/docker.js';

	function ipamValues(item: NetworkSummaryDto, key: 'subnet' | 'gateway'): string[] {
		return (item.ipam?.config ?? []).map((c) => c[key]).filter((v): v is string => !!v);
	}

	// A fixed-width digit key that orders like the backend: IPv4 before IPv6, then by address, then by prefix
	// length. An entry that is not an address sorts after every address.
	function addressSortKey(value: string): string {
		const [address = '', prefix = ''] = value.split('/');
		const octets = address.split('.');
		let family = '1';
		let hex = '';
		if (octets.length === 4 && octets.every((octet) => /^\d{1,3}$/.test(octet))) {
			hex = octets.map((octet) => Number(octet).toString(16).padStart(2, '0')).join('');
		} else if (/^[\da-f:]+$/i.test(address)) {
			const [head = '', tail = ''] = address.split('::');
			const headGroups = head ? head.split(':') : [];
			const tailGroups = tail ? tail.split(':') : [];
			const fill = address.includes('::') ? 8 - headGroups.length - tailGroups.length : 0;
			family = '2';
			hex = [...headGroups, ...Array<string>(Math.max(fill, 0)).fill('0'), ...tailGroups].map((g) => g.padStart(4, '0')).join('');
		}
		if (!hex) return `z${value}`;
		return `${family}${BigInt(`0x${hex}`).toString().padStart(39, '0')}${prefix.padStart(3, '0')}`;
	}

	/** Sort value for a network column whose sort key is not a row field. */
	export function networkSortValue(network: NetworkSummaryDto, column: string): unknown {
		// The backend sorts each network's entries before comparing networks, so entry order does not matter.
		if (column === 'subnet' || column === 'gateway')
			return ipamValues(network, column).map(addressSortKey).sort().join(' ') || null;
		return network[column as keyof NetworkSummaryDto];
	}
</script>

<script lang="ts">
	import { goto } from '$app/navigation';
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
	import { networkService, type NetworksPaginatedResponse } from '#lib/services/network-service.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { bulkConfirmAndRun } from '#lib/utils/bulk-actions.js';
	import {
		environmentHref,
		fetchAcrossEnvironments,
		rowKey,
		type EnvironmentPage,
		type EnvironmentRow
	} from '#lib/utils/environments.js';
	import { capitalizeFirstLetter } from '#lib/utils/formatting.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	type FieldVisibility = Record<string, boolean>;
	type NetworkRow = EnvironmentRow<NetworkSummaryDto>;
	type NetworkPage = EnvironmentPage<NetworksPaginatedResponse>;

	let {
		networks = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable(),
		onNetworksChange,
		onRefreshData
	}: {
		networks: NetworkPage;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
		onNetworksChange?: (networks: NetworkPage) => void;
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
		networks = await fetchAcrossEnvironments(environmentStore.shown, options, (id, request, config) =>
			networkService.getNetworksForEnvironment(id, request, config)
		);
		onNetworksChange?.(networks);
	}

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
							await refreshNetworks();
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
			run: (id, environmentId) => networkService.deleteNetwork(id, environmentId),
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

	const columns = $derived([
		...environmentColumns<NetworkRow>(),
		{ accessorKey: 'id', title: m.common_id(), cell: IdCell, hidden: true },
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'inUse', title: m.common_status(), sortable: true, cell: StatusCell },
		{ accessorKey: 'driver', title: m.common_driver(), sortable: true, cell: DriverCell },
		{ accessorKey: 'scope', title: m.common_scope(), sortable: true, cell: ScopeCell },
		{
			id: 'subnet',
			accessorFn: (row) => networkSortValue(row, 'subnet'),
			title: m.common_subnet(),
			sortable: true,
			cell: SubnetCell
		},
		{
			id: 'gateway',
			accessorFn: (row) => networkSortValue(row, 'gateway'),
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
	<a class="font-medium hover:underline" href={environmentHref(`/networks/${item.id}`, item.environmentId)}>{item.name}</a>
{/snippet}

{#snippet IdCell({ item }: { item: NetworkRow })}
	<span class="truncate font-mono text-sm">{String(item.id)}</span>
{/snippet}

{#snippet DriverCell({ item }: { item: NetworkRow })}
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
		badges={[
			(item: NetworkRow) =>
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
		onclick={() => goto(environmentHref(`/networks/${item.id}`, item.environmentId))}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: NetworkRow })}
	<RowActionsMenu>
		<DropdownMenu.Item onclick={() => goto(environmentHref(`/networks/${item.id}`, item.environmentId))} disabled={isAnyLoading}>
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
