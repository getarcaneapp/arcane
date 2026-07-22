<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import { PortBadge } from '$lib/components/badges';
	import { m } from '$lib/paraglide/messages';
	import type { ContainerDetailsDto } from '$lib/types/docker';
	import { NetworksIcon } from '$lib/icons';

	interface Props {
		container: ContainerDetailsDto;
	}

	let { container }: Props = $props();
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
		</Card.Content>
	</Card.Root>
</div>
