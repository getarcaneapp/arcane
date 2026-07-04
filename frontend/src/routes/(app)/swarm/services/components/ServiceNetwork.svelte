<script lang="ts">
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { m } from '$lib/paraglide/messages';
	import { NetworksIcon, GlobeIcon } from '$lib/icons';
	import DetailPanel from '$lib/components/resource-detail/detail-panel.svelte';
	import DetailSectionCard from '$lib/components/detail-section-card.svelte';
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
		<div class="bg-muted/30 space-y-1 rounded-lg p-2.5">
			<div class="text-muted-foreground mb-1 text-xs font-semibold">{heading}</div>
			{#if cfg.subnet}
				<div class="flex flex-col sm:flex-row sm:items-center">
					<span class="text-muted-foreground w-full text-sm font-medium sm:w-16">{m.common_subnet()}:</span>
					<code
						class="bg-muted text-muted-foreground cursor-pointer rounded px-1.5 py-0.5 font-mono text-xs break-all select-all sm:text-sm"
					>
						{cfg.subnet}
					</code>
				</div>
			{/if}
			{#if cfg.gateway}
				<div class="flex flex-col sm:flex-row sm:items-center">
					<span class="text-muted-foreground w-full text-sm font-medium sm:w-16">{m.common_gateway()}:</span>
					<code
						class="bg-muted text-muted-foreground cursor-pointer rounded px-1.5 py-0.5 font-mono text-xs break-all select-all sm:text-sm"
					>
						{cfg.gateway}
					</code>
				</div>
			{/if}
			{#if cfg.ipRange}
				<div class="flex flex-col sm:flex-row sm:items-center">
					<span class="text-muted-foreground w-full text-sm font-medium sm:w-16">{m.networks_ipam_iprange_label()}:</span>
					<code
						class="bg-muted text-muted-foreground cursor-pointer rounded px-1.5 py-0.5 font-mono text-xs break-all select-all sm:text-sm"
					>
						{cfg.ipRange}
					</code>
				</div>
			{/if}
		</div>
	{/each}
{/snippet}

<DetailPanel>
	<DetailSectionCard icon={GlobeIcon} title={m.common_port_mappings()}>
		{#if ports.length > 0}
			<div class="flex flex-wrap gap-2">
				{#each ports as port (`${port.publishedPort ?? 'internal'}:${port.targetPort}/${port.protocol}:${port.publishMode ?? ''}`)}
					<StatusBadge text={formatPort(port)} variant="gray" size="md" minWidth="none" />
				{/each}
			</div>
		{:else}
			<div class="text-muted-foreground rounded-lg border border-dashed py-12 text-center">
				<div class="text-sm">{m.containers_no_ports()}</div>
			</div>
		{/if}
	</DetailSectionCard>

	<DetailSectionCard icon={NetworksIcon} title={m.swarm_networks()}>
		{#if networks.length > 0 || virtualIPs.length > 0}
			<div class="divide-border/50 divide-y">
				{#each networks as network (network.target)}
					{@const networkId = network.target}
					{@const aliases = network.aliases}
					{@const vip = vipMap[networkId]}
					{@const info = networkDetails[networkId]}
					<div class="space-y-4 py-4 first:pt-0 last:pb-0">
						<div class="flex items-center gap-2">
							<NetworksIcon class="size-4 shrink-0 text-blue-500" />
							<div class="min-w-0 flex-1">
								<div class="text-foreground text-sm font-semibold break-all">
									{info?.name ?? (aliases.length > 0 ? aliases[0] : networkId.slice(0, 12))}
								</div>
								<div class="mt-1 flex flex-wrap items-center gap-2">
									{#if info?.driver}
										<StatusBadge text={info.driver} variant={driverVariant(info.driver)} />
									{/if}
									{#if info?.scope}
										<StatusBadge text={info.scope} variant="gray" />
									{/if}
									{#if info?.internal}
										<StatusBadge text={m.internal()} variant="blue" />
									{/if}
									{#if info?.attachable}
										<StatusBadge text={m.attachable()} variant="green" />
									{/if}
									{#if info?.ingress}
										<StatusBadge text={m.ingress()} variant="cyan" />
									{/if}
									{#if info?.configOnly}
										<StatusBadge text={m.config_only()} variant="pink" />
									{/if}
									{#if info?.configFrom}
										<span class="text-muted-foreground text-xs">{info.configFrom}</span>
									{/if}
								</div>
							</div>
						</div>

						<div class="grid grid-cols-1 gap-x-6 gap-y-4 sm:grid-cols-2">
							{#if vip}
								<div>
									<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
										{m.networks_service_vip_label()}
									</div>
									<div class="text-foreground mt-1 cursor-pointer font-mono text-sm font-medium break-all select-all">
										{vip}
									</div>
								</div>
							{/if}

							<div>
								<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">{m.common_id()}</div>
								<div class="text-foreground mt-1 cursor-pointer font-mono text-sm font-medium break-all select-all">
									{networkId}
								</div>
							</div>

							{#if aliases.length > 0}
								<div>
									<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
										{m.containers_aliases()}
									</div>
									<div class="text-foreground mt-1 space-y-1 text-sm font-medium">
										{#each aliases as alias (alias)}
											<div class="cursor-pointer font-mono break-all select-all">
												{alias}
											</div>
										{/each}
									</div>
								</div>
							{/if}

							{#if info?.configNetwork}
								<div class="sm:col-span-2">
									<div class="border-border/50 mb-3 flex items-center justify-between border-b pb-3">
										<div>
											<div class="text-foreground text-sm font-semibold">
												{m.config_only()}: {info.configNetwork.name}
											</div>
											<div class="mt-1 flex flex-wrap items-center gap-1.5">
												{#if info.configNetwork.driver}
													<StatusBadge text={info.configNetwork.driver} variant="gray" size="sm" minWidth="none" />
												{/if}
												{#if info.configNetwork.scope}
													<StatusBadge text={info.configNetwork.scope} variant="gray" size="sm" minWidth="none" />
												{/if}
												{#if info.configNetwork.options?.['parent']}
													<span class="text-muted-foreground text-xs">{info.configNetwork.options['parent']}</span>
												{/if}
											</div>
										</div>
										<div class="flex items-center gap-2">
											<StatusBadge
												text={info.configNetwork.enableIPv4 ? m.ipv4_enabled() : m.common_disabled()}
												variant={info.configNetwork.enableIPv4 ? 'indigo' : 'gray'}
												size="sm"
												minWidth="none"
											/>
											<StatusBadge
												text={info.configNetwork.enableIPv6 ? m.ipv6_enabled() : m.common_disabled()}
												variant={info.configNetwork.enableIPv6 ? 'indigo' : 'gray'}
												size="sm"
												minWidth="none"
											/>
										</div>
									</div>
									<div class="grid grid-cols-1 gap-x-6 gap-y-4 sm:grid-cols-2">
										{#if info.configNetwork.ipv4Configs && info.configNetwork.ipv4Configs.length > 0}
											{@render IpamConfigList(info.configNetwork.ipv4Configs, m.ipv4_enabled())}
										{/if}
										{#if info.configNetwork.ipv6Configs && info.configNetwork.ipv6Configs.length > 0}
											{@render IpamConfigList(info.configNetwork.ipv6Configs, m.ipv6_enabled())}
										{/if}
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
