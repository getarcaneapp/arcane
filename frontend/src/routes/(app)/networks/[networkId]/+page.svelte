<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import type { PageData } from './$types';
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
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { format } from 'date-fns';
	import { toast } from 'svelte-sonner';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { ArcaneButton } from '$lib/components/arcane-button';
	import { goto } from '$app/navigation';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import { m } from '$lib/paraglide/messages';
	import { networkService } from '$lib/services/network-service';
	import { ResourceDetailLayout, type DetailAction } from '$lib/layouts';

	let { data }: { data: PageData } = $props();
	let errorMessage = $state('');

	let isRemoving = $state(false);
	let sortCol = $state('name');
	let sortDir = $state<'asc' | 'desc'>('asc');

	let network = $derived(data.network);
	const shortId = $derived(network?.id?.substring(0, 12) ?? m.common_unknown());
	const createdDate = $derived(network?.created ? format(new Date(network.created), 'PP p') : m.common_unknown());

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
						onSuccess: async () => {
							toast.success(m.networks_remove_success({ name: network?.name ?? shortId }));
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
	backLabel={m.networks_title()}
	title={network?.name ?? m.common_details_title({ resource: m.resource_network_cap() })}
	subtitle={`${m.common_id()}: ${shortId}`}
	{actions}
>
	{#snippet badges()}
		{#if inUse}
			<StatusBadge variant="green" text={m.networks_in_use_count({ count: connectedContainers.length })} />
		{:else}
			<StatusBadge variant="amber" text={m.common_unused()} />
		{/if}
		{#if isPredefined}
			<StatusBadge variant="blue" text={m.networks_predefined()} />
		{/if}
		<StatusBadge variant="purple" text={network?.driver ?? m.common_unknown()} />
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
						<div class="flex items-start gap-3">
							<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-gray-500/10 p-2">
								<HashIcon class="size-5 text-gray-500" />
							</div>
							<div class="min-w-0 flex-1">
								<p class="text-muted-foreground text-sm font-medium">{m.common_id()}</p>
								<p
									class="mt-1 cursor-pointer font-mono text-xs font-semibold break-all select-all sm:text-sm"
									title="Click to select"
								>
									{network.id}
								</p>
							</div>
						</div>

						<div class="flex items-start gap-3">
							<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-blue-500/10 p-2">
								<NetworksIcon class="size-5 text-blue-500" />
							</div>
							<div class="min-w-0 flex-1">
								<p class="text-muted-foreground text-sm font-medium">{m.common_name()}</p>
								<p class="mt-1 cursor-pointer text-sm font-semibold break-all select-all sm:text-base" title="Click to select">
									{network.name}
								</p>
							</div>
						</div>

						<div class="flex items-start gap-3">
							<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-orange-500/10 p-2">
								<VolumesIcon class="size-5 text-orange-500" />
							</div>
							<div class="min-w-0 flex-1">
								<p class="text-muted-foreground text-sm font-medium">{m.common_driver()}</p>
								<p class="mt-1 cursor-pointer text-sm font-semibold select-all sm:text-base" title="Click to select">
									{network.driver}
								</p>
							</div>
						</div>

						<div class="flex items-start gap-3">
							<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-purple-500/10 p-2">
								<GlobeIcon class="size-5 text-purple-500" />
							</div>
							<div class="min-w-0 flex-1">
								<p class="text-muted-foreground text-sm font-medium">{m.common_scope()}</p>
								<p class="mt-1 cursor-pointer text-sm font-semibold capitalize select-all sm:text-base" title="Click to select">
									{network.scope}
								</p>
							</div>
						</div>

						<div class="flex items-start gap-3">
							<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-green-500/10 p-2">
								<ClockIcon class="size-5 text-green-500" />
							</div>
							<div class="min-w-0 flex-1">
								<p class="text-muted-foreground text-sm font-medium">{m.common_created()}</p>
								<p class="mt-1 cursor-pointer text-sm font-semibold select-all sm:text-base" title="Click to select">
									{createdDate}
								</p>
							</div>
						</div>

						<div class="flex items-start gap-3">
							<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-yellow-500/10 p-2">
								<LayersIcon class="size-5 text-yellow-500" />
							</div>
							<div class="min-w-0 flex-1">
								<p class="text-muted-foreground text-sm font-medium">{m.attachable()}</p>
								<p class="mt-1 text-base font-semibold">
									<StatusBadge
										variant={network.attachable ? 'green' : 'gray'}
										text={network.attachable ? m.common_yes() : m.common_no()}
									/>
								</p>
							</div>
						</div>

						<div class="flex items-start gap-3">
							<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-red-500/10 p-2">
								<SettingsIcon class="size-5 text-red-500" />
							</div>
							<div class="min-w-0 flex-1">
								<p class="text-muted-foreground text-sm font-medium">{m.internal()}</p>
								<p class="mt-1 text-base font-semibold">
									<StatusBadge
										variant={network.internal ? 'blue' : 'gray'}
										text={network.internal ? m.common_yes() : m.common_no()}
									/>
								</p>
							</div>
						</div>

						<div class="flex items-start gap-3">
							<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-indigo-500/10 p-2">
								<NetworksIcon class="size-5 text-indigo-500" />
							</div>
							<div class="min-w-0 flex-1">
								<p class="text-muted-foreground text-sm font-medium">{m.ipv6_enabled()}</p>
								<p class="mt-1 text-base font-semibold">
									<StatusBadge
										variant={network.enableIPv6 ? 'indigo' : 'gray'}
										text={network.enableIPv6 ? m.common_yes() : m.common_no()}
									/>
								</p>
							</div>
						</div>

						<div class="flex items-start gap-3">
							<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-indigo-500/10 p-2">
								<NetworksIcon class="size-5 text-indigo-500" />
							</div>
							<div class="min-w-0 flex-1">
								<p class="text-muted-foreground text-sm font-medium">{m.ipv4_enabled()}</p>
								<p class="mt-1 text-base font-semibold">
									<StatusBadge
										variant={network.enableIPv4 ? 'indigo' : 'gray'}
										text={network.enableIPv4 ? m.common_yes() : m.common_no()}
									/>
								</p>
							</div>
						</div>

						<div class="flex items-start gap-3">
							<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-cyan-500/10 p-2">
								<SettingsIcon class="size-5 text-cyan-500" />
							</div>
							<div class="min-w-0 flex-1">
								<p class="text-muted-foreground text-sm font-medium">{m.ingress()}</p>
								<p class="mt-1 text-base font-semibold">
									<StatusBadge
										variant={network.ingress ? 'cyan' : 'gray'}
										text={network.ingress ? m.common_yes() : m.common_no()}
									/>
								</p>
							</div>
						</div>

						<div class="flex items-start gap-3">
							<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-pink-500/10 p-2">
								<SettingsIcon class="size-5 text-pink-500" />
							</div>
							<div class="min-w-0 flex-1">
								<p class="text-muted-foreground text-sm font-medium">{m.config_only()}</p>
								<p class="mt-1 text-base font-semibold">
									<StatusBadge
										variant={network.configOnly ? 'pink' : 'gray'}
										text={network.configOnly ? m.common_yes() : m.common_no()}
									/>
								</p>
							</div>
						</div>
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
							{#each network.peers as peer}
								<Card.Root variant="subtle">
									<Card.Content class="flex flex-col gap-2 p-4">
										<div class="text-muted-foreground text-xs font-semibold tracking-wide break-all uppercase">{peer.Name}</div>
										<div
											class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all"
											title="Click to select"
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
							<Card.Title>{m.networks_services_title()}</Card.Title>
							<Card.Description>{m.networks_services_description()}</Card.Description>
						</div>
					</Card.Header>
					<Card.Content class="p-4">
						<div class="space-y-3">
							{#each Object.entries(network.services) as [name, service]}
								<Card.Root variant="outlined">
									<Card.Content class="p-4">
										<div class="space-y-2">
											<div class="flex flex-col sm:flex-row sm:items-center">
												<span class="text-muted-foreground w-full text-sm font-medium sm:w-24"
													>{m.networks_service_name_label()}:</span
												>
												<code
													class="bg-muted text-muted-foreground mt-1 rounded px-1.5 py-0.5 font-mono text-xs sm:mt-0 sm:text-sm"
												>
													{name}
												</code>
											</div>
											{#if service.VIP}
												<div class="flex flex-col sm:flex-row sm:items-center">
													<span class="text-muted-foreground w-full text-sm font-medium sm:w-24"
														>{m.networks_service_vip_label()}:</span
													>
													<code
														class="bg-muted text-muted-foreground mt-1 rounded px-1.5 py-0.5 font-mono text-xs sm:mt-0 sm:text-sm"
													>
														{service.VIP}
													</code>
												</div>
											{/if}
											{#if service.Ports && service.Ports.length > 0}
												<div class="flex flex-col sm:flex-row sm:items-start">
													<span class="text-muted-foreground w-full text-sm font-medium sm:w-24"
														>{m.networks_service_ports_label()}:</span
													>
													<div class="flex flex-wrap gap-1">
														{#each service.Ports as port}
															<code class="bg-muted text-muted-foreground rounded px-1.5 py-0.5 font-mono text-xs sm:text-sm">
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
													<span class="text-muted-foreground w-full text-sm font-medium sm:w-24">{m.common_subnet()}:</span>
													<code
														class="bg-muted text-muted-foreground mt-1 cursor-pointer rounded px-1.5 py-0.5 font-mono text-xs select-all sm:mt-0 sm:text-sm"
														title="Click to select"
													>
														{config.subnet}
													</code>
												</div>
											{/if}

											{#if config.gateway}
												<div class="flex flex-col sm:flex-row sm:items-center">
													<span class="text-muted-foreground w-full text-sm font-medium sm:w-24"
														>{m.networks_ipam_gateway_label()}:</span
													>
													<code
														class="bg-muted text-muted-foreground mt-1 cursor-pointer rounded px-1.5 py-0.5 font-mono text-xs select-all sm:mt-0 sm:text-sm"
														title="Click to select"
													>
														{config.gateway}
													</code>
												</div>
											{/if}

											{#if config.ipRange}
												<div class="flex flex-col sm:flex-row sm:items-center">
													<span class="text-muted-foreground w-full text-sm font-medium sm:w-24"
														>{m.networks_ipam_iprange_label()}:</span
													>
													<code
														class="bg-muted text-muted-foreground mt-1 cursor-pointer rounded px-1.5 py-0.5 font-mono text-xs select-all sm:mt-0 sm:text-sm"
														title="Click to select"
													>
														{config.ipRange}
													</code>
												</div>
											{/if}

											{#if config.auxAddress && Object.keys(config.auxAddress).length > 0}
												<div class="mt-3">
													<p class="text-muted-foreground mb-1 text-sm font-medium">{m.networks_ipam_aux_addresses_label()}:</p>
													<ul class="ml-4 space-y-1">
														{#each Object.entries(config.auxAddress) as [name, addr] (name)}
															<li class="flex font-mono text-xs">
																<span class="text-muted-foreground mr-2">{name}:</span>
																<code
																	class="bg-muted text-muted-foreground cursor-pointer rounded px-1 py-0.5 select-all"
																	title="Click to select">{addr}</code
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
								<span class="text-muted-foreground mr-2 text-sm font-medium">{m.networks_ipam_driver_label()}:</span>
								<StatusBadge variant="cyan" text={network.ipam.driver} />
							</div>
						{/if}

						{#if network.ipam.options && Object.keys(network.ipam.options).length > 0}
							<div class="mt-4">
								<p class="text-muted-foreground mb-2 text-sm font-medium">{m.networks_ipam_options_label()}</p>
								<div class="bg-muted/50 rounded-lg border p-3">
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
								<div class="bg-muted/30 flex flex-col border-b p-3 sm:flex-row sm:items-center">
									<div
										class="text-muted-foreground hover:text-foreground flex w-full cursor-pointer items-center text-sm font-medium sm:w-1/3"
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
										class="text-muted-foreground hover:text-foreground flex w-full cursor-pointer items-center pl-0 text-sm font-medium sm:w-2/3 sm:pl-4"
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
												<a href="/containers/{container.id}" class="text-primary flex items-center hover:underline">
													<ContainersIcon class="text-muted-foreground mr-1.5 size-3.5" />
													{container.name ?? container.Name}
												</a>
											</div>
											<div class="w-full pl-0 sm:w-2/3 sm:pl-4">
												<code
													class="bg-muted text-muted-foreground cursor-pointer rounded px-1.5 py-0.5 font-mono text-xs break-all select-all sm:text-sm"
													title="Click to select"
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
				<Card.Root>
					<Card.Header icon={TagIcon}>
						<div class="flex flex-col space-y-1.5">
							<Card.Title>{m.common_labels()}</Card.Title>
							<Card.Description>{m.networks_labels_description()}</Card.Description>
						</div>
					</Card.Header>
					<Card.Content class="p-4">
						<div class="grid grid-cols-1 gap-3 lg:grid-cols-2 2xl:grid-cols-3">
							{#each Object.entries(network.labels) as [key, value] (key)}
								<Card.Root variant="subtle">
									<Card.Content class="flex flex-col gap-2 p-4">
										<div class="text-muted-foreground text-xs font-semibold tracking-wide break-all uppercase">{key}</div>
										<div
											class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all"
											title="Click to select"
										>
											{value}
										</div>
									</Card.Content>
								</Card.Root>
							{/each}
						</div>
					</Card.Content>
				</Card.Root>
			{/if}

			{#if network.options && Object.keys(network.options).length > 0}
				<Card.Root>
					<Card.Header icon={SettingsIcon}>
						<div class="flex flex-col space-y-1.5">
							<Card.Title>{m.networks_options_title()}</Card.Title>
							<Card.Description>{m.networks_options_description()}</Card.Description>
						</div>
					</Card.Header>
					<Card.Content class="p-4">
						<div class="grid grid-cols-1 gap-3 lg:grid-cols-2 2xl:grid-cols-3">
							{#each Object.entries(network.options) as [key, value] (key)}
								<Card.Root variant="subtle">
									<Card.Content class="flex flex-col gap-2 p-4">
										<div class="text-muted-foreground text-xs font-semibold tracking-wide break-all uppercase">{key}</div>
										<div
											class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all"
											title="Click to select"
										>
											{value}
										</div>
									</Card.Content>
								</Card.Root>
							{/each}
						</div>
					</Card.Content>
				</Card.Root>
			{/if}
		</div>
	{:else}
		<div class="flex flex-col items-center justify-center px-4 py-16 text-center">
			<div class="bg-muted/30 mb-4 rounded-full p-4">
				<NetworksIcon class="text-muted-foreground size-10 opacity-70" />
			</div>
			<h2 class="mb-2 text-xl font-medium">{m.common_not_found_title({ resource: m.networks_title() })}</h2>
			<p class="text-muted-foreground mb-6">{m.common_not_found_description({ resource: m.networks_title().toLowerCase() })}</p>
			<ArcaneButton
				action="cancel"
				customLabel={m.common_back_to({ resource: m.networks_title() })}
				onclick={() => goto('/networks')}
				size="sm"
			/>
		</div>
	{/if}
</ResourceDetailLayout>
