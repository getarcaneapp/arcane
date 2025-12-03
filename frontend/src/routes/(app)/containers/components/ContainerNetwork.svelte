<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import NetworkIcon from '@lucide/svelte/icons/network';
	import { m } from '$lib/paraglide/messages';
	import type { ContainerDetailsDto } from '$lib/types/container.type';

	interface NetworkConfig {
		IPAddress?: string;
		IPPrefixLen?: number;
		Gateway?: string;
		MacAddress?: string;
		Aliases?: string[];
		Links?: string[];
		[key: string]: any;
	}

	interface Props {
		container: ContainerDetailsDto;
	}

	let { container }: Props = $props();
</script>

<div class="space-y-6">
	<Card.Root>
		<Card.Header icon={NetworkIcon}>
			<div class="flex flex-col space-y-1.5">
				<Card.Title>
					<h2>
						{m.containers_networks_title()}
					</h2>
				</Card.Title>
				<Card.Description>{m.containers_networks_description()}</Card.Description>
			</div>
		</Card.Header>
		<Card.Content class="p-4">
			{#if container.networkSettings?.networks && Object.keys(container.networkSettings.networks).length > 0}
				<div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
					{#each Object.entries(container.networkSettings.networks) as [networkName, rawNetworkConfig] (networkName)}
						<Card.Root variant="subtle">
							<Card.Content class="p-4">
								<div class="border-border mb-4 flex items-center gap-3 border-b pb-4">
									<div class="rounded-lg bg-blue-500/10 p-2">
										<NetworkIcon class="size-5 text-blue-500" />
									</div>
									<div class="min-w-0 flex-1">
										<div class="text-foreground text-base font-semibold break-all">
											{networkName}
										</div>
										<div class="text-muted-foreground text-xs">{m.network_interface()}</div>
									</div>
								</div>

								<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
									<Card.Root variant="outlined">
										<Card.Content class="flex flex-col p-3">
											<div class="text-muted-foreground mb-2 text-xs font-semibold">
												{m.containers_ip_address()}
											</div>
											<div
												class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all"
												title="Click to select"
											>
												{rawNetworkConfig.ipAddress || m.common_na()}
											</div>
										</Card.Content>
									</Card.Root>

									<Card.Root variant="outlined">
										<Card.Content class="flex flex-col p-3">
											<div class="text-muted-foreground mb-2 text-xs font-semibold">{m.common_gateway()}</div>
											<div
												class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all"
												title="Click to select"
											>
												{rawNetworkConfig.gateway || m.common_na()}
											</div>
										</Card.Content>
									</Card.Root>

									<Card.Root variant="outlined">
										<Card.Content class="flex flex-col p-3">
											<div class="text-muted-foreground mb-2 text-xs font-semibold">
												{m.containers_mac_address()}
											</div>
											<div
												class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all"
												title="Click to select"
											>
												{rawNetworkConfig.macAddress || m.common_na()}
											</div>
										</Card.Content>
									</Card.Root>

									<Card.Root variant="outlined">
										<Card.Content class="flex flex-col p-3">
											<div class="text-muted-foreground mb-2 text-xs font-semibold">{m.common_subnet()}</div>
											<div
												class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all"
												title="Click to select"
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
												<div class="text-muted-foreground mb-2 text-xs font-semibold">Network ID</div>
												<div
													class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all"
													title="Click to select"
												>
													{rawNetworkConfig.networkId}
												</div>
											</Card.Content>
										</Card.Root>
									{/if}

									{#if rawNetworkConfig.endpointId}
										<Card.Root variant="outlined" class="sm:col-span-2">
											<Card.Content class="flex flex-col p-3">
												<div class="text-muted-foreground mb-2 text-xs font-semibold">Endpoint ID</div>
												<div
													class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all"
													title="Click to select"
												>
													{rawNetworkConfig.endpointId}
												</div>
											</Card.Content>
										</Card.Root>
									{/if}

									{#if rawNetworkConfig.aliases && rawNetworkConfig.aliases.length > 0}
										<Card.Root variant="outlined" class="sm:col-span-2">
											<Card.Content class="flex flex-col p-3">
												<div class="text-muted-foreground mb-2 text-xs font-semibold">
													{m.containers_aliases()}
												</div>
												<div class="text-foreground space-y-1 text-sm font-medium">
													{#each rawNetworkConfig.aliases as alias}
														<div class="cursor-pointer font-mono break-all select-all" title="Click to select">
															{alias}
														</div>
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
				<div class="text-muted-foreground rounded-lg border border-dashed py-12 text-center">
					<div class="text-sm">{m.containers_no_networks_connected()}</div>
				</div>
			{/if}
		</Card.Content>
	</Card.Root>
</div>
