<script lang="ts">
	import * as Card from '#lib/components/ui/card/index.js';
	import { PortBadge } from '#lib/components/badges/index.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import SearchableSelect from '#lib/components/form/searchable-select.svelte';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { ContainerDetailsDto } from '#lib/types/docker.js';
	import { NetworksIcon } from '#lib/icons/index.js';
	import { networkService } from '#lib/services/network-service.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { createQuery } from '@tanstack/svelte-query';
	import { refreshAll } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import { extractApiErrorMessage } from '#lib/utils/api.js';

	interface Props {
		container: ContainerDetailsDto;
	}

	let { container }: Props = $props();

	const envId = $derived(environmentStore.selected?.id || '0');
	const canConnect = $derived(hasPermission('networks:connect', envId));
	const canDisconnect = $derived(hasPermission('networks:disconnect', envId));

	let connectNetwork = $state('');
	let connectAliases = $state('');
	let connectIp = $state('');
	let connectPending = $state(false);
	let disconnectPending = $state<Record<string, boolean>>({});

	const networkListOptions = { pagination: { page: 1, limit: 500 } };
	const networksQuery = createQuery(() => ({
		queryKey: queryKeys.networks.list(envId, networkListOptions),
		queryFn: () => networkService.getNetworks(networkListOptions),
		enabled: canConnect
	}));

	const attachedNetworks = $derived(new Set(Object.keys(container.networkSettings?.networks ?? {})));
	const connectableNetworks = $derived(
		(networksQuery.data?.data ?? [])
			.map((network) => network.name)
			.filter((name) => name !== 'host' && name !== 'none' && !attachedNetworks.has(name))
			.map((name) => ({ value: name, label: name }))
	);

	async function handleConnect() {
		if (!connectNetwork) return;
		connectPending = true;
		try {
			const aliases = connectAliases
				.split(',')
				.map((alias) => alias.trim())
				.filter(Boolean);
			await networkService.connectContainer(connectNetwork, {
				containerId: container.id,
				aliases: aliases.length > 0 ? aliases : undefined,
				ipv4Address: connectIp.trim() || undefined
			});
			toast.success(m.network_connect_success());
			connectNetwork = '';
			connectAliases = '';
			connectIp = '';
			await refreshAll();
		} catch (error) {
			toast.error(m.network_connect_failed(), { description: extractApiErrorMessage(error) });
		} finally {
			connectPending = false;
		}
	}

	function handleDisconnect(networkName: string, networkId: string) {
		openConfirmDialog({
			title: m.network_disconnect_confirm_title(),
			message: m.network_disconnect_confirm_message({ network: networkName }),
			confirm: {
				label: m.common_disconnect(),
				destructive: true,
				action: async () => {
					disconnectPending[networkName] = true;
					try {
						await networkService.disconnectContainer(networkId || networkName, { containerId: container.id, force: false });
						toast.success(m.network_disconnect_success());
						await refreshAll();
					} catch (error) {
						toast.error(m.network_disconnect_failed(), { description: extractApiErrorMessage(error) });
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
		<Card.Content class="p-4">
			{#if container.ports && container.ports.length > 0}
				<!-- fallow-ignore-next-line code-duplication -- container vs swarm-service network; typed props diverge across the boundary -->
				<PortBadge ports={container.ports} />
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
		<Card.Content class="p-4">
			{#if container.networkSettings?.networks && Object.keys(container.networkSettings.networks).length > 0}
				<div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
					<!-- fallow-ignore-next-line code-duplication -- container vs swarm-service network; typed props diverge across the boundary -->
					{#each Object.entries(container.networkSettings.networks) as [networkName, rawNetworkConfig] (networkName)}
						<Card.Root variant="subtle">
							<Card.Content class="p-4">
								<div class="mb-4 flex items-center gap-3 border-b border-border pb-4">
									<div class="rounded-lg bg-blue-500/10 p-2">
										<NetworksIcon class="size-5 text-blue-500" />
									</div>
									<div class="min-w-0 flex-1">
										<div class="text-base font-semibold break-all text-foreground">
											{networkName}
										</div>
										<div class="text-xs text-muted-foreground">{m.network_interface()}</div>
									</div>
									{#if canDisconnect}
										<ArcaneButton
											action="base"
											tone="outline"
											size="sm"
											customLabel={m.common_disconnect()}
											loading={disconnectPending[networkName]}
											disabled={disconnectPending[networkName]}
											onclick={() => handleDisconnect(networkName, rawNetworkConfig.networkId)}
											class="shrink-0 text-destructive hover:text-destructive"
										/>
									{/if}
								</div>

								<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
									<Card.Root variant="outlined">
										<Card.Content class="flex flex-col p-3">
											<div class="mb-2 text-xs font-semibold text-muted-foreground">
												{m.containers_ip_address()}
											</div>
											<div
												class="cursor-pointer font-mono text-sm font-medium break-all text-foreground select-all"
												title={m.common_click_to_select()}
											>
												{rawNetworkConfig.ipAddress || m.common_na()}
											</div>
										</Card.Content>
									</Card.Root>

									<Card.Root variant="outlined">
										<Card.Content class="flex flex-col p-3">
											<div class="mb-2 text-xs font-semibold text-muted-foreground">{m.common_gateway()}</div>
											<div
												class="cursor-pointer font-mono text-sm font-medium break-all text-foreground select-all"
												title={m.common_click_to_select()}
											>
												{rawNetworkConfig.gateway || m.common_na()}
											</div>
										</Card.Content>
									</Card.Root>

									<Card.Root variant="outlined">
										<Card.Content class="flex flex-col p-3">
											<div class="mb-2 text-xs font-semibold text-muted-foreground">
												{m.containers_mac_address()}
											</div>
											<div
												class="cursor-pointer font-mono text-sm font-medium break-all text-foreground select-all"
												title={m.common_click_to_select()}
											>
												{rawNetworkConfig.macAddress || m.common_na()}
											</div>
										</Card.Content>
									</Card.Root>

									<Card.Root variant="outlined">
										<Card.Content class="flex flex-col p-3">
											<div class="mb-2 text-xs font-semibold text-muted-foreground">{m.common_subnet()}</div>
											<div
												class="cursor-pointer font-mono text-sm font-medium break-all text-foreground select-all"
												title={m.common_click_to_select()}
											>
												{rawNetworkConfig.ipPrefixLen
													? `${rawNetworkConfig.ipAddress}/${rawNetworkConfig.ipPrefixLen}`
													: m.common_na()}
											</div>
										</Card.Content>
									</Card.Root>

									{#if rawNetworkConfig.networkId}
										<Card.Root variant="outlined" class="sm:col-span-2">
											<Card.Content class="flex flex-col p-3">
												<div class="mb-2 text-xs font-semibold text-muted-foreground">{m.network_id()}</div>
												<div
													class="cursor-pointer font-mono text-sm font-medium break-all text-foreground select-all"
													title={m.common_click_to_select()}
												>
													{rawNetworkConfig.networkId}
												</div>
											</Card.Content>
										</Card.Root>
									{/if}

									{#if rawNetworkConfig.endpointId}
										<Card.Root variant="outlined" class="sm:col-span-2">
											<Card.Content class="flex flex-col p-3">
												<div class="mb-2 text-xs font-semibold text-muted-foreground">{m.container_endpoint_id()}</div>
												<div
													class="cursor-pointer font-mono text-sm font-medium break-all text-foreground select-all"
													title={m.common_click_to_select()}
												>
													{rawNetworkConfig.endpointId}
												</div>
											</Card.Content>
										</Card.Root>
									{/if}

									{#if rawNetworkConfig.aliases && rawNetworkConfig.aliases.length > 0}
										<Card.Root variant="outlined" class="sm:col-span-2">
											<Card.Content class="flex flex-col p-3">
												<div class="mb-2 text-xs font-semibold text-muted-foreground">
													{m.containers_aliases()}
												</div>
												<div class="space-y-1 text-sm font-medium text-foreground">
													{#each rawNetworkConfig.aliases as alias, index (index)}
														<div class="cursor-pointer font-mono break-all select-all" title={m.common_click_to_select()}>
															{alias}
														</div>
														<!-- fallow-ignore-next-line code-duplication -- container vs swarm-service network; typed props diverge across the boundary -->
													{/each}
												</div>
											</Card.Content>
										</Card.Root>
									{/if}
								</div>
							</Card.Content>
						</Card.Root>
					{/each}
				</div>
			{:else}
				<div class="rounded-lg border border-dashed py-12 text-center text-muted-foreground">
					<div class="text-sm">{m.containers_no_networks_connected()}</div>
				</div>
			{/if}

			{#if canConnect}
				<div class="mt-4 space-y-3 rounded-lg border border-border/50 p-4">
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
							class="flex-1 font-mono"
							title={m.aliases_note()}
						/>
						<Input
							type="text"
							placeholder={m.static_ip()}
							bind:value={connectIp}
							disabled={connectPending}
							class="flex-1 font-mono"
						/>
						<ArcaneButton
							action="base"
							tone="outline"
							size="sm"
							customLabel={m.common_connect()}
							loading={connectPending}
							disabled={connectPending || !connectNetwork}
							onclick={handleConnect}
							class="shrink-0"
						/>
					</div>
				</div>
			{/if}
		</Card.Content>
	</Card.Root>
</div>
