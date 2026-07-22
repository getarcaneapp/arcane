<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import { Badge } from '$lib/components/ui/badge';
	import { m } from '$lib/paraglide/messages';
	import { NetworksIcon, GlobeIcon } from '$lib/icons';
	import type { ServiceNetworkAttachment, ServiceNetworkDetail, ServiceVirtualIP, SwarmServicePort } from '$lib/types/swarm';

	interface Props {
		ports: SwarmServicePort[];
		networks: ServiceNetworkAttachment[];
		virtualIPs: ServiceVirtualIP[];
		networkDetails: Record<string, ServiceNetworkDetail>;
	}

	let { ports, networks, virtualIPs, networkDetails }: Props = $props();

	function formatPort(port: SwarmServicePort): string {
		const protocol = port.protocol || 'tcp';
		const target = port.targetPort || 0;
		const published = port.publishedPort || 0;
		const mode = port.publishMode || '';
		if (published) {
			return `${published}:${target}/${protocol}${mode ? ` (${mode})` : ''}`;
		}
		return `${target}/${protocol}`;
	}

	// Match the network detail page's color convention for driver badges
	function driverVariant(driver: string): 'blue' | 'purple' | 'amber' | 'green' | 'gray' {
		if (driver === 'overlay') return 'blue';
		if (driver === 'macvlan') return 'purple';
		if (driver === 'bridge') return 'green';
		if (driver === 'host') return 'amber';
		return 'gray';
	}

	// Build a map of network ID → VIP address
	const vipMap = $derived.by(() => {
		const map: Record<string, string> = {};
		for (const vip of virtualIPs) {
			const id = vip.networkID;
			const addr = vip.addr;
			if (id && addr) map[id] = addr;
		}
		return map;
	});
</script>

{#snippet IpamConfigList(
	configs: NonNullable<NonNullable<ServiceNetworkDetail['configNetwork']>['ipv4Configs']>,
	heading: string
)}
	{#each configs as cfg (`${cfg.subnet ?? ''}:${cfg.gateway ?? ''}:${cfg.ipRange ?? ''}`)}
		<div class="space-y-1 rounded-lg bg-muted/30 p-2.5">
			<div class="mb-1 text-xs font-semibold text-muted-foreground">{heading}</div>
			{#if cfg.subnet}
				<div class="flex flex-col sm:flex-row sm:items-center">
					<span class="w-full text-sm font-medium text-muted-foreground sm:w-16">{m.common_subnet()}:</span>
					<code
						class="cursor-pointer rounded bg-muted px-1.5 py-0.5 font-mono text-xs break-all text-muted-foreground select-all sm:text-sm"
					>
						{cfg.subnet}
					</code>
				</div>
			{/if}
			{#if cfg.gateway}
				<div class="flex flex-col sm:flex-row sm:items-center">
					<span class="w-full text-sm font-medium text-muted-foreground sm:w-16">{m.common_gateway()}:</span>
					<code
						class="cursor-pointer rounded bg-muted px-1.5 py-0.5 font-mono text-xs break-all text-muted-foreground select-all sm:text-sm"
					>
						{cfg.gateway}
					</code>
				</div>
			{/if}
			{#if cfg.ipRange}
				<div class="flex flex-col sm:flex-row sm:items-center">
					<span class="w-full text-sm font-medium text-muted-foreground sm:w-16">{m.networks_ipam_iprange_label()}:</span>
					<code
						class="cursor-pointer rounded bg-muted px-1.5 py-0.5 font-mono text-xs break-all text-muted-foreground select-all sm:text-sm"
					>
						{cfg.ipRange}
					</code>
				</div>
			{/if}
		</div>
	{/each}
{/snippet}

<div class="space-y-6">
	<Card.Root>
		<Card.Header icon={GlobeIcon}>
			<div class="flex flex-col space-y-1.5">
				<Card.Title>
					<h2>{m.common_port_mappings()}</h2>
				</Card.Title>
			</div>
		</Card.Header>
		<Card.Content class="p-4">
			{#if ports.length > 0}
				<div class="flex flex-wrap gap-2">
					{#each ports as port (`${port.publishedPort ?? 'internal'}:${port.targetPort}/${port.protocol}:${port.publishMode ?? ''}`)}
						<Badge variant="gray">{formatPort(port)}</Badge>
					{/each}
				</div>
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
					<h2>{m.resource_networks_cap()}</h2>
				</Card.Title>
			</div>
		</Card.Header>
		<Card.Content class="p-4">
			{#if networks.length > 0 || virtualIPs.length > 0}
				<div class="grid grid-cols-1 gap-4">
					{#each networks as network (network.target)}
						{@const networkId = network.target}
						{@const aliases = network.aliases}
						{@const vip = vipMap[networkId]}
						{@const info = networkDetails[networkId]}
						<Card.Root variant="subtle">
							<Card.Content class="p-4">
								<div class="mb-4 flex items-center gap-3 border-b border-border pb-4">
									<div class="rounded-lg bg-blue-500/10 p-2">
										<NetworksIcon class="size-5 text-blue-500" />
									</div>
									<div class="min-w-0 flex-1">
										<div class="text-base font-semibold break-all text-foreground">
											{info?.name ?? (aliases.length > 0 ? aliases[0] : networkId.slice(0, 12))}
										</div>
										<div class="mt-1 flex flex-wrap items-center gap-2">
											{#if info?.driver}
												<Badge variant={driverVariant(info.driver)} minWidth="20">{info.driver}</Badge>
											{/if}
											{#if info?.scope}
												<Badge variant="gray" minWidth="20">{info.scope}</Badge>
											{/if}
											{#if info?.internal}
												<Badge variant="blue" minWidth="20">{m.internal()}</Badge>
											{/if}
											{#if info?.attachable}
												<Badge variant="green" minWidth="20">{m.attachable()}</Badge>
											{/if}
											{#if info?.ingress}
												<Badge variant="cyan" minWidth="20">{m.ingress()}</Badge>
											{/if}
											{#if info?.configOnly}
												<Badge variant="pink" minWidth="20">{m.config_only()}</Badge>
											{/if}
											{#if info?.configFrom}
												<span class="text-xs text-muted-foreground">{info.configFrom}</span>
											{/if}
										</div>
									</div>
								</div>

								<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
									{#if vip}
										<Card.Root variant="outlined">
											<Card.Content class="flex flex-col p-3">
												<div class="mb-2 text-xs font-semibold text-muted-foreground">{m.networks_service_vip_label()}</div>
												<code
													class="cursor-pointer rounded bg-muted px-1.5 py-0.5 font-mono text-sm break-all text-muted-foreground select-all"
												>
													{vip}
												</code>
											</Card.Content>
										</Card.Root>
									{/if}

									<Card.Root variant="outlined">
										<Card.Content class="flex flex-col p-3">
											<div class="mb-2 text-xs font-semibold text-muted-foreground">{m.common_id()}</div>
											<code
												class="cursor-pointer rounded bg-muted px-1.5 py-0.5 font-mono text-xs break-all text-muted-foreground select-all sm:text-sm"
											>
												{networkId}
											</code>
										</Card.Content>
									</Card.Root>

									{#if aliases.length > 0}
										<Card.Root variant="outlined">
											<Card.Content class="flex flex-col p-3">
												<div class="mb-2 text-xs font-semibold text-muted-foreground">
													{m.containers_aliases()}
												</div>
												<div class="space-y-1">
													{#each aliases as alias (alias)}
														<code
															class="cursor-pointer rounded bg-muted px-1.5 py-0.5 font-mono text-xs break-all text-muted-foreground select-all sm:text-sm"
														>
															{alias}
														</code>
													{/each}
												</div>
											</Card.Content>
										</Card.Root>
									{/if}

									{#if info?.configNetwork}
										<Card.Root variant="outlined" class="sm:col-span-2">
											<Card.Content class="p-3">
												<div class="mb-3 flex items-center justify-between border-b border-border pb-3">
													<div>
														<div class="text-sm font-semibold text-foreground">
															{m.config_only()}: {info.configNetwork.name}
														</div>
														<div class="mt-1 flex flex-wrap items-center gap-1.5">
															{#if info.configNetwork.driver}
																<Badge variant="gray" size="sm">{info.configNetwork.driver}</Badge>
															{/if}
															{#if info.configNetwork.scope}
																<Badge variant="gray" size="sm">{info.configNetwork.scope}</Badge>
															{/if}
															{#if info.configNetwork.options?.['parent']}
																<span class="text-xs text-muted-foreground">{info.configNetwork.options['parent']}</span>
															{/if}
														</div>
													</div>
													<div class="flex items-center gap-2">
														<Badge variant={info.configNetwork.enableIPv4 ? 'indigo' : 'gray'} size="sm"
															>{info.configNetwork.enableIPv4 ? m.ipv4_enabled() : m.common_disabled()}</Badge
														>
														<Badge variant={info.configNetwork.enableIPv6 ? 'indigo' : 'gray'} size="sm"
															>{info.configNetwork.enableIPv6 ? m.ipv6_enabled() : m.common_disabled()}</Badge
														>
													</div>
												</div>
												<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
													{#if info.configNetwork.ipv4Configs && info.configNetwork.ipv4Configs.length > 0}
														{@render IpamConfigList(info.configNetwork.ipv4Configs, m.ipv4_enabled())}
													{/if}
													{#if info.configNetwork.ipv6Configs && info.configNetwork.ipv6Configs.length > 0}
														{@render IpamConfigList(info.configNetwork.ipv6Configs, m.ipv6_enabled())}
													{/if}
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
