<script lang="ts">
	import NetworkIcon from '@lucide/svelte/icons/network';
	import EthernetPortIcon from '@lucide/svelte/icons/ethernet-port';
	import { toast } from 'svelte-sonner';
	import type { NetworkCreateOptions } from 'dockerode';
	import type { NetworkCreateDto } from '$lib/types/network.type';
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
					onSuccess: (data) => (networks = data),
					errorMessage: m.common_refresh_failed({ resource: m.networks_title() })
				},
				counts: {
					fetch: () => networkService.getNetworkUsageCounts(),
					onSuccess: (data) => (networkUsageCounts = data),
					errorMessage: m.common_refresh_failed({ resource: m.networks_title() })
				}
			},
			(v) => (isLoading.refresh = v)
		);
	}

	useEnvironmentRefresh(refresh);

	async function handleCreate(options: NetworkCreateOptions) {
		isLoading.create = true;
		const name = options.Name?.trim() || m.common_unknown();
		const dto: NetworkCreateDto = {
			Driver: options.Driver,
			CheckDuplicate: options.CheckDuplicate,
			Internal: options.Internal,
			Attachable: options.Attachable,
			Ingress: options.Ingress,
			IPAM: options.IPAM,
			EnableIPv6: options.EnableIPv6,
			Options: options.Options,
			Labels: options.Labels
		};
		handleApiResultWithCallbacks({
			result: await tryCatch(networkService.createNetwork(name, dto)),
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
			value: networkUsageCounts.totalNetworks,
			icon: NetworkIcon,
			iconColor: 'text-blue-500',
			class: 'border-l-4 border-l-blue-500'
		},
		{
			title: m.unused_networks(),
			value: networkUsageCounts.networksUnused,
			icon: EthernetPortIcon,
			iconColor: 'text-amber-500',
			class: 'border-l-4 border-l-amber-500'
		}
	]);
</script>

<ResourcePageLayout title={m.networks_title()} subtitle={m.networks_subtitle()} {actionButtons} {statCards} statCardsColumns={2}>
	{#snippet mainContent()}
		<NetworkTable bind:networks bind:selectedIds bind:requestOptions />
	{/snippet}

	{#snippet additionalContent()}
		<CreateNetworkSheet bind:open={isCreateDialogOpen} isLoading={isLoading.create} onSubmit={handleCreate} />
	{/snippet}
</ResourcePageLayout>
