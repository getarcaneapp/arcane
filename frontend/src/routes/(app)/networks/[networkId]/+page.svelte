<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import {
		AlertIcon,
		VolumesIcon,
		ClockIcon,
		TagIcon,
		LayersIcon,
		HashIcon,
		NetworksIcon,
		GlobeIcon,
		SettingsIcon,
		ContainersIcon,
		InfoIcon,
		ArrowUpIcon,
		ArrowDownIcon
	} from '$lib/icons';
	import * as Alert from '$lib/components/ui/alert';
	import { Badge } from '$lib/components/ui/badge';
	import { formatDateTimeShort } from '$lib/utils/formatting';
	import { toast } from 'svelte-sonner';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { ArcaneButton } from '$lib/components/arcane-button';
	import { goto } from '$app/navigation';
	import { handleApiResultWithCallbacks } from '$lib/utils/api';
	import { tryCatch } from '$lib/utils/api';
	import { m } from '$lib/paraglide/messages';
	import { networkService } from '$lib/services/network-service';
	import { ResourceDetailLayout, type DetailAction } from '$lib/layouts';
	import { activityToastOptions, extractActivityId } from '$lib/utils/activity-toast';
	import PropertyItem from '$lib/components/property-item.svelte';
	import KeyValueGridCard from '$lib/components/key-value-grid-card.svelte';

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
			<Card.Root>
				<Card.Header icon={InfoIcon}>
					<div class="flex flex-col space-y-1.5">
						<Card.Title>{m.common_details_title({ resource: m.resource_network_cap() })}</Card.Title>
						<Card.Description>{m.common_details_description({ resource: m.resource_network() })}</Card.Description>
					</div>
				</Card.Header>
				<Card.Content class="p-4">
					<div class="grid grid-cols-1 gap-x-4 gap-y-6 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-6">
						<PropertyItem
							icon={HashIcon}
							color="gray"
							label={m.common_id()}
							value={network.id}
							valueClass="mt-1 cursor-pointer font-mono text-xs font-semibold break-all select-all sm:text-sm"
						/>

						<PropertyItem
							icon={NetworksIcon}
							color="blue"
							label={m.common_name()}
							value={network.name}
							valueClass="mt-1 cursor-pointer text-sm font-semibold break-all select-all sm:text-base"
						/>

						<PropertyItem icon={VolumesIcon} color="orange" label={m.common_driver()} value={network.driver} />

						<PropertyItem
							icon={GlobeIcon}
							color="purple"
							label={m.common_scope()}
							value={network.scope}
							valueClass="mt-1 cursor-pointer text-sm font-semibold capitalize select-all sm:text-base"
						/>

						<PropertyItem icon={ClockIcon} color="green" label={m.common_created()} value={createdDate} />

						<PropertyItem icon={LayersIcon} color="yellow" label={m.attachable()}>
							<p class="mt-1 text-base font-semibold">
								<Badge variant={network.attachable ? 'green' : 'gray'} minWidth="20"
									>{network.attachable ? m.common_yes() : m.common_no()}</Badge
								>
							</p>
						</PropertyItem>

						<PropertyItem icon={SettingsIcon} color="red" label={m.internal()}>
							<p class="mt-1 text-base font-semibold">
								<Badge variant={network.internal ? 'blue' : 'gray'} minWidth="20"
									>{network.internal ? m.common_yes() : m.common_no()}</Badge
								>
							</p>
						</PropertyItem>

						{#snippet ipToggleTile(label: string, enabled: boolean | undefined)}
							<PropertyItem icon={NetworksIcon} color="indigo" {label}>
								<p class="mt-1 text-base font-semibold">
									<Badge variant={enabled ? 'indigo' : 'gray'} minWidth="20">{enabled ? m.common_yes() : m.common_no()}</Badge>
								</p>
							</PropertyItem>
						{/snippet}

						{@render ipToggleTile(m.ipv6_enabled(), network.enableIPv6)}

						{@render ipToggleTile(m.ipv4_enabled(), network.enableIPv4)}

						<PropertyItem icon={SettingsIcon} color="cyan" label={m.ingress()}>
							<p class="mt-1 text-base font-semibold">
								<Badge variant={network.ingress ? 'cyan' : 'gray'} minWidth="20"
									>{network.ingress ? m.common_yes() : m.common_no()}</Badge
								>
							</p>
						</PropertyItem>

						<PropertyItem icon={SettingsIcon} color="pink" label={m.config_only()}>
							<p class="mt-1 text-base font-semibold">
								<Badge variant={network.configOnly ? 'pink' : 'gray'} minWidth="20"
									>{network.configOnly ? m.common_yes() : m.common_no()}</Badge
								>
							</p>
						</PropertyItem>
					</div>
				</Card.Content>
			</Card.Root>

			{#if network.peers && network.peers.length > 0}
				<Card.Root>
					<Card.Header icon={GlobeIcon}>
						<div class="flex flex-col space-y-1.5">
							<Card.Title>{m.networks_peers_title()}</Card.Title>
							<Card.Description>{m.networks_peers_description()}</Card.Description>
						</div>
					</Card.Header>
					<Card.Content class="p-4">
						<div class="grid grid-cols-1 gap-3 lg:grid-cols-2 2xl:grid-cols-3">
							{#each network.peers as peer (`${peer.Name ?? ''}:${peer.IP ?? ''}`)}
								<Card.Root variant="subtle">
									<Card.Content class="flex flex-col gap-2 p-4">
										<div class="text-xs font-semibold tracking-wide break-all text-muted-foreground uppercase">{peer.Name}</div>
										<div
											class="cursor-pointer font-mono text-sm font-medium break-all text-foreground select-all"
											title={m.common_click_to_select()}
										>
											{peer.IP}
										</div>
									</Card.Content>
								</Card.Root>
							{/each}
						</div>
					</Card.Content>
				</Card.Root>
			{/if}

			{#if network.services && Object.keys(network.services).length > 0}
				<Card.Root>
					<Card.Header icon={LayersIcon}>
						<div class="flex flex-col space-y-1.5">
							<Card.Title>{m.services()}</Card.Title>
							<Card.Description>{m.networks_services_description()}</Card.Description>
						</div>
					</Card.Header>
					<Card.Content class="p-4">
						<div class="space-y-3">
							{#each Object.entries(network.services) as [name, service] (name)}
								<Card.Root variant="outlined">
									<Card.Content class="p-4">
										<div class="space-y-2">
											<div class="flex flex-col sm:flex-row sm:items-center">
												<span class="w-full text-sm font-medium text-muted-foreground sm:w-24">{m.common_name()}:</span>
												<code
													class="mt-1 rounded bg-muted px-1.5 py-0.5 font-mono text-xs text-muted-foreground sm:mt-0 sm:text-sm"
												>
													{name}
												</code>
											</div>
											{#if service.VIP}
												<div class="flex flex-col sm:flex-row sm:items-center">
													<span class="w-full text-sm font-medium text-muted-foreground sm:w-24"
														>{m.networks_service_vip_label()}:</span
													>
													<code
														class="mt-1 rounded bg-muted px-1.5 py-0.5 font-mono text-xs text-muted-foreground sm:mt-0 sm:text-sm"
													>
														{service.VIP}
													</code>
												</div>
											{/if}
											{#if service.Ports && service.Ports.length > 0}
												<div class="flex flex-col sm:flex-row sm:items-start">
													<span class="w-full text-sm font-medium text-muted-foreground sm:w-24">{m.common_ports()}:</span>
													<div class="flex flex-wrap gap-1">
														{#each service.Ports as port (port)}
															<code class="rounded bg-muted px-1.5 py-0.5 font-mono text-xs text-muted-foreground sm:text-sm">
																{port}
															</code>
														{/each}
													</div>
												</div>
											{/if}
										</div>
									</Card.Content>
								</Card.Root>
							{/each}
						</div>
					</Card.Content>
				</Card.Root>
			{/if}

			{#if network.ipam?.config && network.ipam.config.length > 0}
				<Card.Root>
					<Card.Header icon={SettingsIcon}>
						<div class="flex flex-col space-y-1.5">
							<Card.Title>{m.networks_ipam_title()}</Card.Title>
							<Card.Description>{m.networks_ipam_description()}</Card.Description>
						</div>
					</Card.Header>
					<Card.Content class="p-4">
						<div class="space-y-3">
							{#each network.ipam.config as config, i (i)}
								<Card.Root variant="outlined">
									<Card.Content class="p-4">
										<div class="space-y-2">
											{#if config.subnet}
												<div class="flex flex-col sm:flex-row sm:items-center">
													<span class="w-full text-sm font-medium text-muted-foreground sm:w-24">{m.common_subnet()}:</span>
													<code
														class="mt-1 cursor-pointer rounded bg-muted px-1.5 py-0.5 font-mono text-xs text-muted-foreground select-all sm:mt-0 sm:text-sm"
														title={m.common_click_to_select()}
													>
														{config.subnet}
													</code>
												</div>
											{/if}

											{#if config.gateway}
												<div class="flex flex-col sm:flex-row sm:items-center">
													<span class="w-full text-sm font-medium text-muted-foreground sm:w-24">{m.common_gateway()}:</span>
													<code
														class="mt-1 cursor-pointer rounded bg-muted px-1.5 py-0.5 font-mono text-xs text-muted-foreground select-all sm:mt-0 sm:text-sm"
														title={m.common_click_to_select()}
													>
														{config.gateway}
													</code>
												</div>
											{/if}

											{#if config.ipRange}
												<div class="flex flex-col sm:flex-row sm:items-center">
													<span class="w-full text-sm font-medium text-muted-foreground sm:w-24"
														>{m.networks_ipam_iprange_label()}:</span
													>
													<code
														class="mt-1 cursor-pointer rounded bg-muted px-1.5 py-0.5 font-mono text-xs text-muted-foreground select-all sm:mt-0 sm:text-sm"
														title={m.common_click_to_select()}
													>
														{config.ipRange}
													</code>
												</div>
											{/if}

											{#if config.auxAddress && Object.keys(config.auxAddress).length > 0}
												<div class="mt-3">
													<p class="mb-1 text-sm font-medium text-muted-foreground">{m.networks_ipam_aux_addresses_label()}:</p>
													<ul class="ml-4 space-y-1">
														{#each Object.entries(config.auxAddress) as [name, addr] (name)}
															<li class="flex font-mono text-xs">
																<span class="mr-2 text-muted-foreground">{name}:</span>
																<code
																	class="cursor-pointer rounded bg-muted px-1 py-0.5 text-muted-foreground select-all"
																	title={m.common_click_to_select()}>{addr}</code
																>
															</li>
														{/each}
													</ul>
												</div>
											{/if}
										</div>
									</Card.Content>
								</Card.Root>
							{/each}
						</div>

						{#if network.ipam.driver}
							<div class="mt-4 flex items-center">
								<span class="mr-2 text-sm font-medium text-muted-foreground">{m.networks_ipam_driver_label()}:</span>
								<Badge variant="cyan" minWidth="20">{network.ipam.driver}</Badge>
							</div>
						{/if}

						{#if network.ipam.options && Object.keys(network.ipam.options).length > 0}
							<div class="mt-4">
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
					</Card.Content>
				</Card.Root>
			{/if}

			{#if connectedContainers.length > 0}
				<Card.Root>
					<Card.Header icon={ContainersIcon}>
						<div class="flex flex-col space-y-1.5">
							<Card.Title>{m.networks_connected_containers_title()}</Card.Title>
							<Card.Description
								>{m.networks_connected_containers_description({ count: connectedContainers.length })}</Card.Description
							>
						</div>
					</Card.Header>
					<Card.Content class="p-4">
						<Card.Root variant="outlined">
							<Card.Content class="p-0">
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
							</Card.Content>
						</Card.Root>
					</Card.Content>
				</Card.Root>
			{/if}

			{#if network.labels && Object.keys(network.labels).length > 0}
				<KeyValueGridCard
					icon={TagIcon}
					title={m.common_labels()}
					description={m.networks_labels_description()}
					entries={Object.entries(network.labels)}
				/>
			{/if}

			{#if network.options && Object.keys(network.options).length > 0}
				<KeyValueGridCard
					icon={SettingsIcon}
					title={m.networks_options_title()}
					description={m.networks_options_description()}
					entries={Object.entries(network.options)}
				/>
			{/if}
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
