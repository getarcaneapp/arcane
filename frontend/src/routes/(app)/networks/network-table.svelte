<script lang="ts">
	import type { NetworkSummaryDto } from '$lib/types/network.type';
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { goto } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import { DEFAULT_NETWORK_NAMES } from '$lib/constants';
	import type { SearchPaginationSortRequest, Paginated } from '$lib/types/pagination.type';
	import { capitalizeFirstLetter } from '$lib/utils/string.utils';
	import type { ColumnSpec } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table';
	import { m } from '$lib/paraglide/messages';
	import { networkService } from '$lib/services/network-service';
	import { NetworksIcon, GlobeIcon, EllipsisIcon, InspectIcon, TrashIcon } from '$lib/icons';

	type FieldVisibility = Record<string, boolean>;

	let {
		networks = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable()
	}: {
		networks: Paginated<NetworkSummaryDto>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
	} = $props();

	let isLoading = $state({
		remove: false
	});

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
						onSuccess: async () => {
							toast.success(m.common_delete_success({ resource: `${m.resource_network()} "${safeName}"` }));
							networks = await networkService.getNetworks(requestOptions);
						}
					});
				}
			}
		});
	}

	async function handleDeleteSelectedNetworks(ids: string[]) {
		const selectedNetworkList = networks.data.filter((n) => ids.includes(n.id));
		const defaultNetworks = selectedNetworkList.filter((n) => n.isDefault);

		if (defaultNetworks.length > 0) {
			const names = defaultNetworks.map((n) => n.name ?? m.common_unknown()).join(', ');
			toast.error(m.networks_cannot_delete_default_many({ names }));
			return;
		}

		openConfirmDialog({
			title: m.networks_delete_selected_title({ count: ids.length }),
			message: m.networks_delete_selected_message({ count: ids.length }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					isLoading.remove = true;
					let successCount = 0;
					let failureCount = 0;

					for (const networkId of ids) {
						const network = networks.data.find((n) => n.id === networkId);
						if (!network) continue;

						const result = await tryCatch(networkService.deleteNetwork(networkId));
						if (result.error) {
							failureCount++;
							toast.error(
								m.common_delete_failed({ resource: `${m.resource_network()} "${network.name ?? m.common_unknown()}"` })
							);
						} else {
							successCount++;
							toast.success(
								m.common_delete_success({
									resource: `${m.resource_network()} "${network.name ?? m.common_unknown()}"`
								})
							);
						}
					}

					isLoading.remove = false;

					if (successCount > 0) {
						networks = await networkService.getNetworks(requestOptions);
					}
					selectedIds = [];
				}
			}
		});
	}

	const isAnyLoading = $derived(Object.values(isLoading).some((l) => l));
	const hasNetworks = $derived(networks?.data?.length > 0);

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

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet NameCell({ item }: { item: NetworkSummaryDto })}
	<a class="font-medium hover:underline" href="/networks/{item.id}">{item.name}</a>
{/snippet}

{#snippet IdCell({ item }: { item: NetworkSummaryDto })}
	<span class="truncate font-mono text-sm">{String(item.id)}</span>
{/snippet}

{#snippet DriverCell({ item }: { item: NetworkSummaryDto })}
	<StatusBadge
		variant={item.driver === 'bridge'
			? 'blue'
			: item.driver === 'overlay'
				? 'purple'
				: item.driver === 'ipvlan'
					? 'red'
					: item.driver === 'macvlan'
						? 'orange'
						: 'gray'}
		text={capitalizeFirstLetter(item.driver)}
	/>
{/snippet}

{#snippet ScopeCell({ item }: { item: NetworkSummaryDto })}
	<StatusBadge variant={item.scope === 'local' ? 'green' : 'amber'} text={capitalizeFirstLetter(item.scope)} />
{/snippet}

{#snippet StatusCell({ item }: { item: NetworkSummaryDto })}
	{#if item.isDefault}
		<StatusBadge text={m.networks_predefined()} variant="sky" />
	{:else if item.inUse}
		<StatusBadge text={m.common_in_use()} variant="green" />
	{:else}
		<StatusBadge text={m.common_unused()} variant="amber" />
	{/if}
{/snippet}

{#snippet NetworkMobileCardSnippet({
	row,
	item,
	mobileFieldVisibility
}: {
	row: any;
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
		subtitle={(item: NetworkSummaryDto) => ((mobileFieldVisibility.id ?? true) ? item.id : null)}
		badges={[
			(item: NetworkSummaryDto) =>
				(mobileFieldVisibility.inUse ?? true)
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
				show: mobileFieldVisibility.driver ?? true
			},
			{
				label: m.common_scope(),
				getValue: (item: NetworkSummaryDto) => capitalizeFirstLetter(item.scope),
				icon: GlobeIcon,
				iconVariant: 'gray' as const,
				type: 'badge' as const,
				badgeVariant: item.scope === 'local' ? ('green' as const) : ('amber' as const),
				show: mobileFieldVisibility.scope ?? true
			}
		]}
		rowActions={RowActions}
		onclick={() => goto(`/networks/${item.id}`)}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: NetworkSummaryDto })}
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
			</DropdownMenu.Group>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/snippet}

<ArcaneTable
	persistKey="arcane-networks-table"
	items={networks}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	onRemoveSelected={(ids) => handleDeleteSelectedNetworks(ids)}
	onRefresh={async (options) => (networks = await networkService.getNetworks(options))}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={NetworkMobileCardSnippet}
/>
