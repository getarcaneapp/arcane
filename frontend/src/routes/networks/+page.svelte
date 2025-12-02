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
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import type { Environment } from '$lib/types/environment.type';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';

	let { data } = $props();

	let networks = $state(untrack(() => data.networks));
	let networkUsageCounts = $state(untrack(() => data.networkUsageCounts));
	let requestOptions = $state(untrack(() => data.networkRequestOptions));
	let selectedIds = $state<string[]>([]);
	let isCreateDialogOpen = $state(false);

	let isLoading = $state({
		create: false,
		refresh: false
	});

	const totalNetworks = $derived(networkUsageCounts.totalNetworks);
	const unusedNetworks = $derived(networkUsageCounts.networksUnused);

	async function refreshNetworks() {
		isLoading.refresh = true;
		let refreshingNetworkList = true;
		let refreshingNetworkCounts = true;
		handleApiResultWithCallbacks({
			result: await tryCatch(networkService.getNetworks(requestOptions)),
			message: m.common_refresh_failed({ resource: m.networks_title() }),
			setLoadingState: (value) => {
				refreshingNetworkList = value;
				isLoading.refresh = refreshingNetworkCounts || refreshingNetworkList;
			},
			async onSuccess(newNetworks) {
				networks = newNetworks;
			}
		});
		handleApiResultWithCallbacks({
			result: await tryCatch(networkService.getNetworkUsageCounts()),
			message: m.common_refresh_failed({ resource: m.networks_title() }),
			setLoadingState: (value) => {
				refreshingNetworkCounts = value;
				isLoading.refresh = refreshingNetworkCounts || refreshingNetworkList;
			},
			async onSuccess(newNetworkCounts) {
				networkUsageCounts = newNetworkCounts;
			}
		});
	}

	async function handleCreateNetworkSubmit(options: NetworkCreateOptions) {
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
			setLoadingState: (value) => (isLoading.create = value),
			onSuccess: async () => {
				toast.success(m.common_create_success({ resource: `${m.resource_network()} "${name}"` }));
				networks = await networkService.getNetworks(requestOptions);
				isCreateDialogOpen = false;
			}
		});
	}

	let lastEnvId: string | null = null;
	$effect(() => {
		const env = environmentStore.selected;
		if (!env) return;
		if (lastEnvId === null) {
			lastEnvId = env.id;
			return;
		}
		if (env.id !== lastEnvId) {
			lastEnvId = env.id;
			refreshNetworks();
		}
	});

	const actionButtons: ActionButton[] = $derived.by(() => [
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
			onclick: refreshNetworks,
			loading: isLoading.refresh,
			disabled: isLoading.refresh
		}
	]);

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.networks_total(),
			value: totalNetworks,
			icon: NetworkIcon,
			iconColor: 'text-blue-500',
			class: 'border-l-4 border-l-blue-500'
		},
		{
			title: m.unused_networks(),
			value: unusedNetworks,
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
		<CreateNetworkSheet bind:open={isCreateDialogOpen} isLoading={isLoading.create} onSubmit={handleCreateNetworkSubmit} />
	{/snippet}
</ResourcePageLayout>
