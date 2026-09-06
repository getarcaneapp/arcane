<script lang="ts">
	import type { PageProps } from './$types';
	import type { NetworkInspectDto, IPAMSubnetDto } from '#lib/types/docker.js';
	import {
		AlertIcon,
		VolumesIcon,
		ClockIcon,
		TagIcon,
		LayersIcon,
		NetworksIcon,
		GlobeIcon,
		SettingsIcon,
		ContainersIcon,
		ArrowUpIcon,
		ArrowDownIcon,
		type IconType
	} from '#lib/icons/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { formatDateTimeShort } from '#lib/utils/formatting.js';
	import { toast } from 'svelte-sonner';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { goto } from '$app/navigation';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/api.js';
	import { m } from '#lib/paraglide/messages.js';
	import { networkService } from '#lib/services/network-service.js';
	import { ResourceDetailLayout, type DetailAction } from '#lib/layouts/index.js';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast.js';
	import { DetailMetaStrip, DetailSection, KeyValueCard, KeyValueGrid } from '#lib/components/resource-detail/index.js';

	let { data }: PageProps = $props();
	let errorMessage = $state('');

	let isRemoving = $state(false);
	let sortCol = $state('name');
	let sortDir = $state<'asc' | 'desc'>('asc');

	let network = $derived(data.network);
	const shortId = $derived(network?.id?.substring(0, 12) ?? m.common_unknown());
	const createdDate = $derived(network?.created ? formatDateTimeShort(network.created) : m.common_unknown());

	const connectedContainers = $derived(
		network?.containersList ??
			(network?.containers ? Object.entries(network.containers).map(([id, info]) => ({ id, ...(info as any) })) : [])
	);

	const inUse = $derived(connectedContainers.length > 0);
	const isPredefined = $derived(network?.name === 'bridge' || network?.name === 'host' || network?.name === 'none');

	async function handleSort(column: string) {
		const newSortDir: 'asc' | 'desc' = sortCol === column && sortDir === 'asc' ? 'desc' : 'asc';
		sortCol = column;
		sortDir = newSortDir;

		if (data.network?.id) {
			try {
				network = await networkService.getNetwork(data.network.id, {
					sort: { column: sortCol, direction: sortDir }
				});
			} catch (err) {
				console.error('Failed to sort network containers:', err);
				toast.error(m.common_action_failed());
			}
		}
	}

	function triggerRemove() {
		if (isPredefined) {
			toast.error(m.networks_cannot_delete_default({ name: network?.name ?? m.common_unknown() }));
			console.warn('Cannot remove predefined network');
			return;
		}

		if (!network?.id) {
			toast.error(m.networks_missing_id ? m.networks_missing_id() : m.error_occurred());
			return;
		}

		openConfirmDialog({
			title: m.common_remove_title({ resource: m.resource_network() }),
			message: m.networks_remove_confirm_message({ name: network?.name ?? shortId }),
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
					handleApiResultWithCallbacks({
						result: await tryCatch(networkService.deleteNetwork(network.id)),
						message: m.networks_remove_failed({ name: network?.name ?? shortId }),
						setLoadingState: (value) => (isRemoving = value),
						onSuccess: async (data) => {
							toast.success(
								m.networks_remove_success({ name: network?.name ?? shortId }),
								activityToastOptions(extractActivityId(data))
							);
							goto('/networks');
						},
						onError: (error) => {
							errorMessage = error?.message ?? m.error_occurred();
							toast.error(errorMessage);
						}
					});
				}
			}
		});
	}

	const actions: DetailAction[] = $derived([
		{
			id: 'remove',
			action: 'remove',
			label: m.common_remove(),
			loading: isRemoving,
			disabled: isRemoving || isPredefined,
			onclick: triggerRemove
		}
	]);
</script>

{#snippet networkContainerSorting()}
	<div class="flex flex-col border-b bg-muted/30 p-3 sm:flex-row sm:items-center">
		<div
			class="flex w-full cursor-pointer items-center text-sm font-medium text-muted-foreground hover:text-foreground sm:w-1/3"
			onclick={() => handleSort('name')}
			role="button"
			tabindex="0"
			onkeydown={(e) => e.key === 'Enter' && handleSort('name')}
		>
			{m.common_name()}
			{#if sortCol === 'name'}
				{#if sortDir === 'asc'}
					<ArrowUpIcon class="ml-1 size-3" />
				{:else}
					<ArrowDownIcon class="ml-1 size-3" />
				{/if}
			{/if}
		</div>
		<div
			class="flex w-full cursor-pointer items-center pl-0 text-sm font-medium text-muted-foreground hover:text-foreground sm:w-2/3 sm:pl-4"
			onclick={() => handleSort('ip')}
			role="button"
			tabindex="0"
			onkeydown={(e) => e.key === 'Enter' && handleSort('ip')}
		>
			{m.containers_ip_address()}
			{#if sortCol === 'ip'}
				{#if sortDir === 'asc'}
					<ArrowUpIcon class="ml-1 size-3" />
				{:else}
					<ArrowDownIcon class="ml-1 size-3" />
				{/if}
			{/if}
		</div>
	</div>
{/snippet}

{#snippet networkPeers(network: NetworkInspectDto)}
	{#if network.peers && network.peers.length > 0}
		<DetailSection title={m.networks_peers_title()} icon={GlobeIcon}>
			<KeyValueGrid>
				{#each network.peers as peer (`${peer.Name ?? ''}:${peer.IP ?? ''}`)}
					<KeyValueCard label={peer.Name ?? m.common_unknown()} valueTitle={m.common_click_to_select()}>
						{peer.IP}
					</KeyValueCard>
				{/each}
			</KeyValueGrid>
		</DetailSection>
	{/if}
{/snippet}

{#snippet ipamSubnet(config: IPAMSubnetDto)}
	<KeyValueGrid>
		{#if config.subnet}
			<KeyValueCard label={m.common_subnet()} valueTitle={m.common_click_to_select()}>{config.subnet}</KeyValueCard>
		{/if}
		{#if config.gateway}
			<KeyValueCard label={m.common_gateway()} valueTitle={m.common_click_to_select()}>{config.gateway}</KeyValueCard>
		{/if}
		{#if config.ipRange}
			<KeyValueCard label={m.networks_ipam_iprange_label()} valueTitle={m.common_click_to_select()}>
				{config.ipRange}
			</KeyValueCard>
		{/if}
		{#if config.auxAddress && Object.keys(config.auxAddress).length > 0}
			{#each Object.entries(config.auxAddress) as [name, addr] (name)}
				<KeyValueCard label={name} valueTitle={m.common_click_to_select()}>{addr}</KeyValueCard>
			{/each}
		{/if}
	</KeyValueGrid>
{/snippet}

{#snippet networkServices(network: NetworkInspectDto)}
	{#if network.services && Object.keys(network.services).length > 0}
		<DetailSection title={m.services()} icon={LayersIcon}>
			<div class="divide-y rounded-lg border">
				{#each Object.entries(network.services) as [name, service] (name)}
					<div class="flex flex-col gap-2 p-3 sm:flex-row sm:flex-wrap sm:items-center sm:gap-4">
						<code class="cursor-pointer font-mono text-sm font-medium break-all select-all" title={m.common_click_to_select()}>
							{name}
						</code>
						{#if service.VIP}
							<span class="flex items-center gap-1.5 text-xs text-muted-foreground">
								{m.networks_service_vip_label()}
								<code class="rounded bg-muted px-1.5 py-0.5 font-mono text-xs break-all select-all">{service.VIP}</code>
							</span>
						{/if}
						{#if service.Ports && service.Ports.length > 0}
							<span class="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
								{m.common_ports()}
								{#each service.Ports as port (port)}
									<code class="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">{port}</code>
								{/each}
							</span>
						{/if}
					</div>
				{/each}
			</div>
		</DetailSection>
	{/if}
{/snippet}

{#snippet networkIpam(network: NetworkInspectDto)}
	{#if network.ipam?.config && network.ipam.config.length > 0}
		<DetailSection title={m.networks_ipam_title()} icon={SettingsIcon}>
			<div class="space-y-3">
				{#each network.ipam.config as config, i (i)}
					{@render ipamSubnet(config)}
				{/each}
			</div>

			{#if network.ipam.driver}
				<div class="flex items-center gap-2">
					<span class="text-sm font-medium text-muted-foreground">{m.networks_ipam_driver_label()}:</span>
					<Badge variant="cyan" minWidth="20">{network.ipam.driver}</Badge>
				</div>
			{/if}

			{#if network.ipam.options && Object.keys(network.ipam.options).length > 0}
				<div>
					<p class="mb-2 text-sm font-medium text-muted-foreground">{m.networks_ipam_options_label()}</p>
					<div class="rounded-lg border bg-muted/50 p-3">
						{#each Object.entries(network.ipam.options) as [key, value] (key)}
							<div class="mb-1 flex justify-between font-mono text-xs last:mb-0">
								<span class="text-muted-foreground">{key}:</span>
								<span>{value}</span>
							</div>
						{/each}
					</div>
				</div>
			{/if}
		</DetailSection>
	{/if}
{/snippet}

{#snippet networkContainers()}
	{#if connectedContainers.length > 0}
		<DetailSection title={m.networks_connected_containers_title()} icon={ContainersIcon}>
			<div class="overflow-hidden rounded-lg border">
				{@render networkContainerSorting()}

				<div class="divide-y">
					{#each connectedContainers as container (container.id)}
						<div class="flex flex-col p-3 sm:flex-row sm:items-center">
							<div class="mb-2 w-full font-medium break-all sm:mb-0 sm:w-1/3">
								<a href="/containers/{container.id}" class="flex items-center text-primary hover:underline">
									<ContainersIcon class="mr-1.5 size-3.5 text-muted-foreground" />
									{container.name ?? container.Name}
								</a>
							</div>
							<div class="w-full pl-0 sm:w-2/3 sm:pl-4">
								<code
									class="cursor-pointer rounded bg-muted px-1.5 py-0.5 font-mono text-xs break-all text-muted-foreground select-all sm:text-sm"
									title={m.common_click_to_select()}
								>
									{container.ipv4Address ??
										container.IPv4Address ??
										container.ipv6Address ??
										container.IPv6Address ??
										m.common_unknown()}
								</code>
							</div>
						</div>
					{/each}
				</div>
			</div>
		</DetailSection>
	{/if}
{/snippet}

{#snippet keyValueSection(title: string, icon: IconType, values: Record<string, string> | null | undefined)}
	{#if values && Object.keys(values).length > 0}
		<DetailSection {title} {icon}>
			<KeyValueGrid>
				{#each Object.entries(values) as [key, value] (key)}
					<KeyValueCard label={key} valueTitle={m.common_click_to_select()}>{value}</KeyValueCard>
				{/each}
			</KeyValueGrid>
		</DetailSection>
	{/if}
{/snippet}

<ResourceDetailLayout
	backUrl="/networks"
	backLabel={m.resource_networks_cap()}
	title={network?.name ?? m.common_details_title({ resource: m.resource_network_cap() })}
	subtitle={`${m.common_id()}: ${shortId}`}
	{actions}
>
	{#snippet badges()}
		{#if inUse}
			<Badge variant="green" minWidth="20">{m.networks_in_use_count({ count: connectedContainers.length })}</Badge>
		{:else}
			<Badge variant="amber" minWidth="20">{m.common_unused()}</Badge>
		{/if}
		{#if isPredefined}
			<Badge variant="blue" minWidth="20">{m.networks_predefined()}</Badge>
		{/if}
		<Badge variant="purple" minWidth="20">{network?.driver ?? m.common_unknown()}</Badge>
	{/snippet}

	{#if errorMessage}
		<Alert.Root variant="destructive">
			<AlertIcon class="mr-2 size-4" />
			<Alert.Title>{m.common_action_failed()}</Alert.Title>
			<Alert.Description>{errorMessage}</Alert.Description>
		</Alert.Root>
	{/if}

	{#if network}
		<div class="space-y-6">
			<DetailMetaStrip
				items={[
					{ icon: VolumesIcon, label: m.common_driver(), value: network.driver ?? m.common_unknown() },
					{ icon: GlobeIcon, label: m.common_scope(), value: network.scope ?? m.common_unknown() },
					{ icon: ClockIcon, value: createdDate }
				]}
			>
				{#if network.attachable}
					<Badge variant="green" minWidth="20">{m.attachable()}</Badge>
				{/if}
				{#if network.internal}
					<Badge variant="blue" minWidth="20">{m.internal()}</Badge>
				{/if}
				{#if network.ingress}
					<Badge variant="cyan" minWidth="20">{m.ingress()}</Badge>
				{/if}
				{#if network.enableIPv6}
					<Badge variant="indigo" minWidth="20">{m.ipv6_enabled()}</Badge>
				{/if}
				{#if network.configOnly}
					<Badge variant="pink" minWidth="20">{m.config_only()}</Badge>
				{/if}
			</DetailMetaStrip>

			<KeyValueCard label={m.common_id()} valueTitle={m.common_click_to_select()}>{network.id}</KeyValueCard>

			{@render networkPeers(network)}

			{@render networkServices(network)}

			{@render networkIpam(network)}

			{@render networkContainers()}

			{@render keyValueSection(m.common_labels(), TagIcon, network.labels)}
			{@render keyValueSection(m.networks_options_title(), SettingsIcon, network.options)}
		</div>
	{:else}
		<div class="flex flex-col items-center justify-center px-4 py-16 text-center">
			<div class="mb-4 rounded-full bg-muted/30 p-4">
				<NetworksIcon class="size-10 text-muted-foreground opacity-70" />
			</div>
			<h2 class="mb-2 text-xl font-medium">{m.common_not_found_title({ resource: m.resource_networks_cap() })}</h2>
			<p class="mb-6 text-muted-foreground">
				{m.common_not_found_description({ resource: m.resource_networks_cap().toLowerCase() })}
			</p>
			<ArcaneButton
				action="cancel"
				customLabel={m.common_back_to({ resource: m.resource_networks_cap() })}
				onclick={() => goto('/networks')}
				size="sm"
			/>
		</div>
	{/if}
</ResourceDetailLayout>
