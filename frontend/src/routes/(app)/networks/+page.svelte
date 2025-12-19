<script lang="ts">
	import { NetworksIcon, ConnectionIcon } from '$lib/icons';
	import { toast } from 'svelte-sonner';
	import type { NetworkCreateOptions } from '$lib/types/network.type';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import CreateNetworkSheet from '$lib/components/sheets/create-network-sheet.svelte';
	import NetworkTable from './network-table.svelte';
	import { m } from '$lib/paraglide/messages';
	import { networkService } from '$lib/services/network-service';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import { useEnvironmentRefresh } from '$lib/hooks/use-environment-refresh.svelte';
	import { parallelRefresh } from '$lib/utils/refresh.util';

	let { data } = $props();

	let networks = $state(untrack(() => data.networks));
	let networkUsageCounts = $state(untrack(() => data.networkUsageCounts));
	let requestOptions = $state(untrack(() => data.networkRequestOptions));
	let selectedIds = $state<string[]>([]);
	let isCreateDialogOpen = $state(false);
	let isLoading = $state({ create: false, refresh: false });

	async function refresh() {
		await parallelRefresh(
			{
				networks: {
					fetch: () => networkService.getNetworks(requestOptions),
					onSuccess: (data) => {
						networks = data;
						networkUsageCounts = data.counts ?? { inuse: 0, unused: 0, total: 0 };
					},
					errorMessage: m.common_refresh_failed({ resource: m.networks_title() })
				}
			},
			(v) => (isLoading.refresh = v)
		);
	}

	useEnvironmentRefresh(refresh);

	async function handleCreate(name: string, options: NetworkCreateOptions) {
		isLoading.create = true;
		handleApiResultWithCallbacks({
			result: await tryCatch(networkService.createNetwork(name, options)),
			message: m.common_create_failed({ resource: `${m.resource_network()} "${name}"` }),
			setLoadingState: (v) => (isLoading.create = v),
			onSuccess: async () => {
				toast.success(m.common_create_success({ resource: `${m.resource_network()} "${name}"` }));
				networks = await networkService.getNetworks(requestOptions);
				isCreateDialogOpen = false;
			}
		});
	}

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'create',
			action: 'create',
			label: m.common_create_button({ resource: m.resource_network_cap() }),
			onclick: () => (isCreateDialogOpen = true)
		},
		{
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refresh,
			loading: isLoading.refresh,
			disabled: isLoading.refresh
		}
	]);

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.networks_total(),
			value: networkUsageCounts.total,
			icon: NetworksIcon,
			iconColor: 'text-blue-500'
		},
		{
			title: m.unused_networks(),
			value: networkUsageCounts.unused,
			icon: ConnectionIcon,
			iconColor: 'text-amber-500'
		}
	]);
</script>

<ResourcePageLayout title={m.networks_title()} subtitle={m.networks_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<NetworkTable bind:networks bind:selectedIds bind:requestOptions />
	{/snippet}

	{#snippet additionalContent()}
		<CreateNetworkSheet bind:open={isCreateDialogOpen} isLoading={isLoading.create} onSubmit={handleCreate} />
	{/snippet}
</ResourcePageLayout>
