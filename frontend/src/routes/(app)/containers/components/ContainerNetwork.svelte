<script lang="ts">
	import { PortBadge } from '$lib/components/badges';
	import { m } from '$lib/paraglide/messages';
	import type { ContainerDetailsDto } from '$lib/types/docker';
	import { NetworksIcon } from '$lib/icons';
	import DetailPanel from '$lib/components/resource-detail/detail-panel.svelte';
	import DetailSectionCard from '$lib/components/detail-section-card.svelte';

	interface Props {
		container: ContainerDetailsDto;
	}

	let { container }: Props = $props();
</script>

{#snippet netValue(label: string, value: string, spanClass?: string)}
	<div class={spanClass}>
		<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">{label}</div>
		<div
			class="text-foreground mt-1 cursor-pointer font-mono text-sm font-medium break-all select-all"
			title={m.common_click_to_select()}
		>
			{value}
		</div>
	</div>
{/snippet}

<DetailPanel>
	<!-- fallow-ignore-next-line code-duplication container vs swarm-service network; typed props diverge across the boundary -->
	<DetailSectionCard id="container-port-mappings" icon={NetworksIcon} title={m.common_port_mappings()}>
		{#if container.ports && container.ports.length > 0}
			<!-- fallow-ignore-next-line code-duplication container vs swarm-service network; typed props diverge across the boundary -->
			<PortBadge ports={container.ports} />
		{:else}
			<div class="text-muted-foreground rounded-lg border border-dashed py-12 text-center">
				<div class="text-sm">{m.containers_no_ports()}</div>
			</div>
		{/if}
	</DetailSectionCard>

	<DetailSectionCard icon={NetworksIcon} title={m.containers_networks_title()} description={m.containers_networks_description()}>
		{#if container.networkSettings?.networks && Object.keys(container.networkSettings.networks).length > 0}
			<div class="divide-border/50 divide-y">
				<!-- fallow-ignore-next-line code-duplication container vs swarm-service network; typed props diverge across the boundary -->
				{#each Object.entries(container.networkSettings.networks) as [networkName, rawNetworkConfig] (networkName)}
					<div class="space-y-4 py-4 first:pt-0 last:pb-0">
						<div class="flex items-center gap-2">
							<NetworksIcon class="size-4 shrink-0 text-blue-500" />
							<div class="min-w-0 flex-1">
								<span class="text-foreground text-sm font-semibold break-all">{networkName}</span>
								<span class="text-muted-foreground ml-2 text-xs">{m.network_interface()}</span>
							</div>
						</div>

						<div class="grid grid-cols-1 gap-x-6 gap-y-4 sm:grid-cols-2">
							{@render netValue(m.containers_ip_address(), rawNetworkConfig.ipAddress || m.common_na())}
							{@render netValue(m.common_gateway(), rawNetworkConfig.gateway || m.common_na())}
							{@render netValue(m.containers_mac_address(), rawNetworkConfig.macAddress || m.common_na())}
							{@render netValue(
								m.common_subnet(),
								rawNetworkConfig.ipPrefixLen ? `${rawNetworkConfig.ipAddress}/${rawNetworkConfig.ipPrefixLen}` : m.common_na()
							)}
							{#if rawNetworkConfig.networkId}
								{@render netValue(m.container_network_id(), rawNetworkConfig.networkId, 'sm:col-span-2')}
							{/if}
							{#if rawNetworkConfig.endpointId}
								{@render netValue(m.container_endpoint_id(), rawNetworkConfig.endpointId, 'sm:col-span-2')}
							{/if}
							{#if rawNetworkConfig.aliases && rawNetworkConfig.aliases.length > 0}
								<div class="sm:col-span-2">
									<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
										{m.containers_aliases()}
									</div>
									<div class="text-foreground mt-1 space-y-1 text-sm font-medium">
										{#each rawNetworkConfig.aliases as alias, index (index)}
											<div class="cursor-pointer font-mono break-all select-all" title={m.common_click_to_select()}>
												{alias}
											</div>
											<!-- fallow-ignore-next-line code-duplication container vs swarm-service network; typed props diverge across the boundary -->
										{/each}
									</div>
								</div>
							{/if}
						</div>
					</div>
				{/each}
			</div>
		{:else}
			<div class="text-muted-foreground rounded-lg border border-dashed py-12 text-center">
				<div class="text-sm">{m.containers_no_networks_connected()}</div>
			</div>
		{/if}
	</DetailSectionCard>
</DetailPanel>
