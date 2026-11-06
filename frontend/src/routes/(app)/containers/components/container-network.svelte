<script lang="ts">
	import { refreshAll } from '$app/navigation';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { PortBadge } from '#lib/components/badges/index.js';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import SearchableSelect from '#lib/components/form/searchable-select.svelte';
	import { KeyValueCard, KeyValueGrid } from '#lib/components/resource-detail/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { NetworksIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { networkService } from '#lib/services/network-service.js';
	import type { ContainerDetails } from '#lib/types/container.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { getCustomHostname } from '#lib/utils/docker.js';
	import { withOccurrenceKeys } from '#lib/utils/formatting.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	interface Props {
		container: ContainerDetails;
		environmentId: string;
	}

	let { container, environmentId }: Props = $props();

	const queryClient = useQueryClient();
	const envId = $derived(environmentId);
	const customHostname = $derived(getCustomHostname(container));
	const canConnect = $derived(hasPermission('networks:connect', envId));
	const canDisconnect = $derived(hasPermission('networks:disconnect', envId));

	let connectNetwork = $state('');
	let connectAliases = $state('');
	let connectIp = $state('');
	let connectPending = $state(false);
	const disconnectPending = $state<Record<string, boolean>>({});

	const networkListOptions = { pagination: { page: 1, limit: 500 } };
	const networksQuery = createQuery(() => ({
		queryKey: queryKeys.networks.list(envId, networkListOptions),
		queryFn: () => networkService.getNetworksForEnvironment(envId, networkListOptions),
		enabled: canConnect
	}));

	const attachedNetworks = $derived(new Set(Object.keys(container.networkSettings?.networks ?? {})));
	const connectableNetworks = $derived(
		(networksQuery.data?.data ?? [])
			.map((network) => network.name)
			.filter((name) => name !== 'host' && name !== 'none' && !attachedNetworks.has(name))
			.map((name) => ({ value: name, label: name }))
	);

	// The loader reads the container through the query cache, so a refresh within its stale window would
	// reuse the detail fetched just before the change and leave the network list on screen out of date.
	async function refreshContainer(targetEnvId: string, targetContainerId: string) {
		await queryClient.invalidateQueries({ queryKey: queryKeys.containers.detail(targetEnvId, targetContainerId) });
		await refreshAll();
	}

	async function handleConnect() {
		if (!connectNetwork) return;
		connectPending = true;
		try {
			const operationResult = await tryCatch(
				(async () => {
					const aliases = connectAliases
						.split(',')
						.map((alias) => alias.trim())
						.filter(Boolean);
					await networkService.connectContainer(
						connectNetwork,
						{
							containerId: container.id,
							aliases: aliases.length > 0 ? aliases : undefined,
							ipv4Address: connectIp.trim() || undefined
						},
						envId
					);
					toast.success(m.network_connect_success());
					connectNetwork = '';
					connectAliases = '';
					connectIp = '';
					await refreshContainer(envId, container.id);
				})()
			);
			await handleApiResultWithCallbacks({ result: operationResult, message: m.network_connect_failed() });
		} finally {
			connectPending = false;
		}
	}

	function handleDisconnect(networkName: string, networkId: string) {
		// The dialog outlives navigation, so it disconnects the container it was opened for.
		const target = { containerId: container.id, environmentId: envId };
		openConfirmDialog({
			title: m.network_disconnect_confirm_title(),
			message: m.network_disconnect_confirm_message({ network: networkName }),
			confirm: {
				label: m.common_disconnect(),
				destructive: true,
				action: async () => {
					disconnectPending[networkName] = true;
					try {
						const operationResult = await tryCatch(
							(async () => {
								await networkService.disconnectContainer(
									networkId || networkName,
									{ containerId: target.containerId, force: false },
									target.environmentId
								);
								toast.success(m.network_disconnect_success());
								await refreshContainer(target.environmentId, target.containerId);
							})()
						);
						await handleApiResultWithCallbacks({ result: operationResult, message: m.network_disconnect_failed() });
					} finally {
						disconnectPending[networkName] = false;
					}
				}
			}
		});
	}
</script>

<div class="space-y-6">
	<Card.Root id="container-port-mappings">
		<!-- fallow-ignore-next-line code-duplication -- container vs swarm-service network; typed props diverge across the boundary -->
		<Card.Header icon={NetworksIcon}>
			<div class="flex flex-col space-y-1.5">
				<Card.Title>
					<h2>
						{m.common_port_mappings()}
					</h2>
				</Card.Title>
			</div>
		</Card.Header>
		<Card.Content>
			{#if container.ports && container.ports.length > 0}
				<!-- fallow-ignore-next-line code-duplication -- container vs swarm-service network; typed props diverge across the boundary -->
				<PortBadge ports={container.ports} environmentId={envId} />
			{:else}
				<div class="rounded-lg border border-dashed py-12 text-center text-muted-foreground">
					<div class="text-sm">{m.containers_no_ports()}</div>
				</div>
			{/if}
		</Card.Content>
	</Card.Root>

	<Card.Root>
		<Card.Header icon={NetworksIcon}>
			<div class="flex flex-col space-y-1.5">
				<Card.Title>
					<h2>
						{m.resource_networks_cap()}
					</h2>
				</Card.Title>
				<Card.Description>{m.containers_networks_description()}</Card.Description>
			</div>
		</Card.Header>
		<Card.Content>
			{#if container.networkSettings?.networks && Object.keys(container.networkSettings.networks).length > 0}
				<div class="divide-y divide-border/60">
					<!-- fallow-ignore-next-line code-duplication -- container vs swarm-service network; typed props diverge across the boundary -->
					{#each Object.entries(container.networkSettings.networks) as [networkName, rawNetworkConfig] (networkName)}
						<section class="py-5 first:pt-0 last:pb-0">
							<div class="flex items-center gap-3 pb-1">
								<NetworksIcon class="size-4 shrink-0 text-info" />
								<div class="min-w-0 flex-1">
									<div class="text-sm font-semibold break-all text-foreground">{networkName}</div>
									<div class="text-xs text-muted-foreground">{m.network_interface()}</div>
								</div>
								{#if canDisconnect}
									<ArcaneButton
										action="base"
										tone="outline"
										customLabel={m.common_disconnect()}
										loading={disconnectPending[networkName]}
										disabled={disconnectPending[networkName]}
										onclick={() => handleDisconnect(networkName, rawNetworkConfig.networkId || networkName)}
										class="shrink-0 text-destructive hover:text-destructive"
									/>
								{/if}
							</div>

							<KeyValueGrid>
								{#each [{ label: m.containers_ip_address(), value: rawNetworkConfig.ipAddress || m.common_na() }, { label: m.common_gateway(), value: rawNetworkConfig.gateway || m.common_na() }, { label: m.containers_mac_address(), value: rawNetworkConfig.macAddress || m.common_na() }, { label: m.common_subnet(), value: rawNetworkConfig.ipPrefixLen ? `${rawNetworkConfig.ipAddress}/${rawNetworkConfig.ipPrefixLen}` : m.common_na() }] as field (field.label)}
									<KeyValueCard label={field.label} valueTitle={m.common_click_to_select()}>{field.value}</KeyValueCard>
								{/each}

								{#if rawNetworkConfig.globalIPv6Address}
									<KeyValueCard label={m.ipv6_address()} valueTitle={m.common_click_to_select()}>
										{rawNetworkConfig.globalIPv6PrefixLen
											? `${rawNetworkConfig.globalIPv6Address}/${rawNetworkConfig.globalIPv6PrefixLen}`
											: rawNetworkConfig.globalIPv6Address}
									</KeyValueCard>
									<KeyValueCard label={m.ipv6_gateway()} valueTitle={m.common_click_to_select()}>
										{rawNetworkConfig.ipv6Gateway || m.common_na()}
									</KeyValueCard>
								{/if}

								{#if rawNetworkConfig.networkId}
									<KeyValueCard
										label={m.network_id()}
										valueTitle={m.common_click_to_select()}
										class="lg:col-span-2 2xl:col-span-3"
									>
										{rawNetworkConfig.networkId}
									</KeyValueCard>
								{/if}

								{#if rawNetworkConfig.endpointId}
									<KeyValueCard
										label={m.container_endpoint_id()}
										valueTitle={m.common_click_to_select()}
										class="lg:col-span-2 2xl:col-span-3"
									>
										{rawNetworkConfig.endpointId}
									</KeyValueCard>
								{/if}

								{#if rawNetworkConfig.aliases && rawNetworkConfig.aliases.length > 0}
									<KeyValueCard label={m.containers_aliases()} stacked class="lg:col-span-2 2xl:col-span-3">
										{#each withOccurrenceKeys(rawNetworkConfig.aliases) as alias (alias.key)}
											<div class="cursor-pointer font-mono break-all select-all" title={m.common_click_to_select()}>
												{alias.value}
											</div>
											<!-- fallow-ignore-next-line code-duplication -- container vs swarm-service network; typed props diverge across the boundary -->
										{/each}
									</KeyValueCard>
								{/if}
							</KeyValueGrid>
						</section>
					{/each}
				</div>
			{:else}
				<div class="rounded-lg border border-dashed py-12 text-center text-muted-foreground">
					<div class="text-sm">{m.containers_no_networks_connected()}</div>
				</div>
			{/if}

			{#if canConnect}
				<div class="mt-5 space-y-3 border-t border-border/60 pt-5">
					<div>
						<h3 class="text-sm font-semibold">{m.network_connect()}</h3>
						<p class="mt-1 text-xs text-muted-foreground">{m.network_live_change_note()}</p>
					</div>
					<div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:gap-3">
						<SearchableSelect
							items={connectableNetworks}
							bind:value={connectNetwork}
							showCheckboxes={false}
							disabled={connectPending}
							class="min-w-44 flex-1"
						/>
						<Input
							type="text"
							placeholder={m.containers_aliases()}
							bind:value={connectAliases}
							disabled={connectPending}
							mono
							class="flex-1"
							title={m.aliases_note()}
						/>
						<Input type="text" placeholder={m.static_ip()} bind:value={connectIp} disabled={connectPending} mono class="flex-1" />
						<ArcaneButton
							action="base"
							tone="outline"
							customLabel={m.common_connect()}
							loading={connectPending}
							disabled={connectPending || !connectNetwork}
							onclick={handleConnect}
							class="shrink-0"
						/>
					</div>
				</div>
			{/if}

			{#if customHostname}
				<KeyValueGrid>
					<KeyValueCard label={m.hostname()} valueTitle={m.common_click_to_select()}>{customHostname}</KeyValueCard>
				</KeyValueGrid>
			{/if}
		</Card.Content>
	</Card.Root>
</div>
