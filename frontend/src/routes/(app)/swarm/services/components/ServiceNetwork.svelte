<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { m } from '$lib/paraglide/messages';
	import { NetworksIcon, GlobeIcon, SettingsIcon } from '$lib/icons';
	import { networkService } from '$lib/services/network-service';
	import type { NetworkInspectDto } from '$lib/types/network.type';

	interface Props {
		ports: any[];
		networks: any[];
		virtualIPs: any[];
	}

	let { ports, networks, virtualIPs }: Props = $props();

	let networkDetails = $state<Map<string, NetworkInspectDto>>(new Map());
	let isLoadingNetworks = $state(false);

	function formatPort(port: any): string {
		const protocol = port.Protocol || port.protocol || 'tcp';
		const target = port.TargetPort || port.targetPort || 0;
		const published = port.PublishedPort || port.publishedPort || 0;
		const mode = port.PublishMode || port.publishMode || '';
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
		const map = new Map<string, string>();
		for (const vip of virtualIPs) {
			const id = vip.NetworkID || vip.networkID || '';
			const addr = vip.Addr || vip.addr || '';
			if (id && addr) map.set(id, addr);
		}
		return map;
	});

	async function loadNetworkDetails() {
		isLoadingNetworks = true;
		const details = new Map<string, NetworkInspectDto>();

		const networkIds = networks.map((n: any) => n.Target || n.target || '').filter(Boolean);

		await Promise.allSettled(
			networkIds.map(async (id: string) => {
				try {
					const info = await networkService.getNetwork(id);
					details.set(id, info);

					// For macvlan/config-based networks, also fetch the config network for IPAM details
					if (info.configFrom?.Network && (!info.ipam?.config || info.ipam.config.length === 0)) {
						try {
							const configInfo = await networkService.getNetwork(info.configFrom.Network);
							details.set(`${id}:config`, configInfo);
						} catch {
							// Config network may not be accessible
						}
					}
				} catch {
					// Network may have been removed
				}
			})
		);

		networkDetails = details;
		isLoadingNetworks = false;
	}

	$effect(() => {
		if (networks.length > 0) {
			loadNetworkDetails();
		}
	});

	function getNetworkInfo(networkId: string) {
		const info = networkDetails.get(networkId);
		const configInfo = networkDetails.get(`${networkId}:config`);
		const ipamSource = configInfo?.ipam?.config?.length ? configInfo : info;
		const ipamConfigs = ipamSource?.ipam?.config ?? [];
		const ipv4Configs = ipamConfigs.filter((c) => c.subnet && !c.subnet.includes(':'));
		const ipv6Configs = ipamConfigs.filter((c) => c.subnet && c.subnet.includes(':'));

		return {
			name: info?.name ?? null,
			driver: info?.driver ?? null,
			scope: info?.scope ?? null,
			internal: info?.internal ?? false,
			attachable: info?.attachable ?? false,
			ingress: info?.ingress ?? false,
			enableIPv4: info?.enableIPv4 ?? false,
			enableIPv6: info?.enableIPv6 ?? false,
			configFrom: info?.configFrom?.Network ?? null,
			configOnly: info?.configOnly ?? false,
			ipamDriver: ipamSource?.ipam?.driver ?? null,
			ipv4Configs,
			ipv6Configs,
			configNetwork: configInfo
				? {
						name: configInfo.name,
						driver: configInfo.driver,
						scope: configInfo.scope,
						enableIPv4: configInfo.enableIPv4 ?? false,
						enableIPv6: configInfo.enableIPv6 ?? false,
						options: configInfo.options,
						ipv4Configs: (configInfo.ipam?.config ?? []).filter((c) => c.subnet && !c.subnet.includes(':')),
						ipv6Configs: (configInfo.ipam?.config ?? []).filter((c) => c.subnet && c.subnet.includes(':'))
					}
				: null
		};
	}
</script>

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
					{#each ports as port}
						<StatusBadge text={formatPort(port)} variant="gray" size="md" minWidth="none" />
					{/each}
				</div>
			{:else}
				<div class="text-muted-foreground rounded-lg border border-dashed py-12 text-center">
					<div class="text-sm">{m.containers_no_ports()}</div>
				</div>
			{/if}
		</Card.Content>
	</Card.Root>

	<Card.Root>
		<Card.Header icon={NetworksIcon}>
			<div class="flex flex-col space-y-1.5">
				<Card.Title>
					<h2>{m.swarm_networks()}</h2>
				</Card.Title>
			</div>
		</Card.Header>
		<Card.Content class="p-4">
			{#if networks.length > 0 || virtualIPs.length > 0}
				<div class="grid grid-cols-1 gap-4">
					{#each networks as network}
						{@const networkId = network.Target || network.target || ''}
						{@const aliases = network.Aliases || network.aliases || []}
						{@const vip = vipMap.get(networkId)}
						{@const info = getNetworkInfo(networkId)}
						<Card.Root variant="subtle">
							<Card.Content class="p-4">
								<div class="border-border mb-4 flex items-center gap-3 border-b pb-4">
									<div class="rounded-lg bg-blue-500/10 p-2">
										<NetworksIcon class="size-5 text-blue-500" />
									</div>
									<div class="min-w-0 flex-1">
										<div class="text-foreground text-base font-semibold break-all">
											{info.name ?? (aliases.length > 0 ? aliases[0] : networkId.slice(0, 12))}
										</div>
										<div class="mt-1 flex flex-wrap items-center gap-2">
											{#if info.driver}
												<StatusBadge text={info.driver} variant={driverVariant(info.driver)} />
											{/if}
											{#if info.scope}
												<StatusBadge text={info.scope} variant="gray" />
											{/if}
											{#if info.internal}
												<StatusBadge text={m.internal()} variant="blue" />
											{/if}
											{#if info.attachable}
												<StatusBadge text={m.attachable()} variant="green" />
											{/if}
											{#if info.ingress}
												<StatusBadge text={m.ingress()} variant="cyan" />
											{/if}
											{#if info.configOnly}
												<StatusBadge text={m.config_only()} variant="pink" />
											{/if}
											{#if info.configFrom}
												<span class="text-muted-foreground text-xs">config: {info.configFrom}</span>
											{/if}
										</div>
									</div>
								</div>

								<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
									{#if vip}
										<Card.Root variant="outlined">
											<Card.Content class="flex flex-col p-3">
												<div class="text-muted-foreground mb-2 text-xs font-semibold">Virtual IP</div>
												<code
													class="bg-muted text-muted-foreground cursor-pointer rounded px-1.5 py-0.5 font-mono text-sm break-all select-all"
													title="Click to select"
												>
													{vip}
												</code>
											</Card.Content>
										</Card.Root>
									{/if}

									{#if info.ipv4Configs.length > 0}
										<Card.Root variant="outlined">
											<Card.Content class="flex flex-col p-3">
												<div class="text-muted-foreground mb-2 flex items-center gap-2 text-xs font-semibold">
													IPv4
													<StatusBadge
														text={info.enableIPv4 ? m.common_yes() : m.common_no()}
														variant={info.enableIPv4 ? 'indigo' : 'gray'}
														size="sm"
														minWidth="none"
													/>
												</div>
												{#each info.ipv4Configs as cfg}
													<div class="mb-2 space-y-1 last:mb-0">
														<div class="flex flex-col sm:flex-row sm:items-center">
															<span class="text-muted-foreground w-full text-sm font-medium sm:w-16">{m.common_subnet()}:</span>
															<code
																class="bg-muted text-muted-foreground mt-1 cursor-pointer rounded px-1.5 py-0.5 font-mono text-xs break-all select-all sm:mt-0 sm:text-sm"
																title="Click to select"
															>
																{cfg.subnet}
															</code>
														</div>
														{#if cfg.gateway}
															<div class="flex flex-col sm:flex-row sm:items-center">
																<span class="text-muted-foreground w-full text-sm font-medium sm:w-16">{m.common_gateway()}:</span
																>
																<code
																	class="bg-muted text-muted-foreground mt-1 cursor-pointer rounded px-1.5 py-0.5 font-mono text-xs break-all select-all sm:mt-0 sm:text-sm"
																	title="Click to select"
																>
																	{cfg.gateway}
																</code>
															</div>
														{/if}
														{#if cfg.ipRange}
															<div class="flex flex-col sm:flex-row sm:items-center">
																<span class="text-muted-foreground w-full text-sm font-medium sm:w-16">Range:</span>
																<code
																	class="bg-muted text-muted-foreground mt-1 cursor-pointer rounded px-1.5 py-0.5 font-mono text-xs break-all select-all sm:mt-0 sm:text-sm"
																	title="Click to select"
																>
																	{cfg.ipRange}
																</code>
															</div>
														{/if}
													</div>
												{/each}
											</Card.Content>
										</Card.Root>
									{/if}

									{#if info.ipv6Configs.length > 0}
										<Card.Root variant="outlined">
											<Card.Content class="flex flex-col p-3">
												<div class="text-muted-foreground mb-2 flex items-center gap-2 text-xs font-semibold">
													IPv6
													<StatusBadge
														text={info.enableIPv6 ? m.common_yes() : m.common_no()}
														variant={info.enableIPv6 ? 'indigo' : 'gray'}
														size="sm"
														minWidth="none"
													/>
												</div>
												{#each info.ipv6Configs as cfg}
													<div class="mb-2 space-y-1 last:mb-0">
														<div class="flex flex-col sm:flex-row sm:items-center">
															<span class="text-muted-foreground w-full text-sm font-medium sm:w-16">{m.common_subnet()}:</span>
															<code
																class="bg-muted text-muted-foreground mt-1 cursor-pointer rounded px-1.5 py-0.5 font-mono text-xs break-all select-all sm:mt-0 sm:text-sm"
																title="Click to select"
															>
																{cfg.subnet}
															</code>
														</div>
														{#if cfg.gateway}
															<div class="flex flex-col sm:flex-row sm:items-center">
																<span class="text-muted-foreground w-full text-sm font-medium sm:w-16">{m.common_gateway()}:</span
																>
																<code
																	class="bg-muted text-muted-foreground mt-1 cursor-pointer rounded px-1.5 py-0.5 font-mono text-xs break-all select-all sm:mt-0 sm:text-sm"
																	title="Click to select"
																>
																	{cfg.gateway}
																</code>
															</div>
														{/if}
														{#if cfg.ipRange}
															<div class="flex flex-col sm:flex-row sm:items-center">
																<span class="text-muted-foreground w-full text-sm font-medium sm:w-16">Range:</span>
																<code
																	class="bg-muted text-muted-foreground mt-1 cursor-pointer rounded px-1.5 py-0.5 font-mono text-xs break-all select-all sm:mt-0 sm:text-sm"
																	title="Click to select"
																>
																	{cfg.ipRange}
																</code>
															</div>
														{/if}
													</div>
												{/each}
											</Card.Content>
										</Card.Root>
									{/if}

									<Card.Root variant="outlined">
										<Card.Content class="flex flex-col p-3">
											<div class="text-muted-foreground mb-2 text-xs font-semibold">Network ID</div>
											<code
												class="bg-muted text-muted-foreground cursor-pointer rounded px-1.5 py-0.5 font-mono text-xs break-all select-all sm:text-sm"
												title="Click to select"
											>
												{networkId}
											</code>
										</Card.Content>
									</Card.Root>

									{#if aliases.length > 0}
										<Card.Root variant="outlined">
											<Card.Content class="flex flex-col p-3">
												<div class="text-muted-foreground mb-2 text-xs font-semibold">
													{m.containers_aliases()}
												</div>
												<div class="space-y-1">
													{#each aliases as alias}
														<code
															class="bg-muted text-muted-foreground cursor-pointer rounded px-1.5 py-0.5 font-mono text-xs break-all select-all sm:text-sm"
															title="Click to select"
														>
															{alias}
														</code>
													{/each}
												</div>
											</Card.Content>
										</Card.Root>
									{/if}

									{#if info.configNetwork}
										<Card.Root variant="outlined" class="sm:col-span-2">
											<Card.Content class="p-3">
												<div class="border-border mb-3 flex items-center justify-between border-b pb-3">
													<div>
														<div class="text-foreground text-sm font-semibold">{m.config_only()}: {info.configNetwork.name}</div>
														<div class="mt-1 flex flex-wrap items-center gap-1.5">
															{#if info.configNetwork.driver}
																<StatusBadge text={info.configNetwork.driver} variant="gray" size="sm" minWidth="none" />
															{/if}
															{#if info.configNetwork.scope}
																<StatusBadge text={info.configNetwork.scope} variant="gray" size="sm" minWidth="none" />
															{/if}
															{#if info.configNetwork.options?.parent}
																<span class="text-muted-foreground text-xs">parent: {info.configNetwork.options.parent}</span>
															{/if}
														</div>
													</div>
													<div class="flex items-center gap-2">
														<StatusBadge
															text="IPv4 {info.configNetwork.enableIPv4 ? 'on' : 'off'}"
															variant={info.configNetwork.enableIPv4 ? 'indigo' : 'gray'}
															size="sm"
															minWidth="none"
														/>
														<StatusBadge
															text="IPv6 {info.configNetwork.enableIPv6 ? 'on' : 'off'}"
															variant={info.configNetwork.enableIPv6 ? 'indigo' : 'gray'}
															size="sm"
															minWidth="none"
														/>
													</div>
												</div>
												<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
													{#if info.configNetwork.ipv4Configs.length > 0}
														{#each info.configNetwork.ipv4Configs as cfg}
															<div class="bg-muted/30 space-y-1 rounded-lg p-2.5">
																<div class="text-muted-foreground mb-1 text-xs font-semibold">IPv4</div>
																<div class="flex flex-col sm:flex-row sm:items-center">
																	<span class="text-muted-foreground w-full text-sm font-medium sm:w-16"
																		>{m.common_subnet()}:</span
																	>
																	<code
																		class="bg-muted text-muted-foreground cursor-pointer rounded px-1.5 py-0.5 font-mono text-xs break-all select-all sm:text-sm"
																		title="Click to select"
																	>
																		{cfg.subnet}
																	</code>
																</div>
																{#if cfg.gateway}
																	<div class="flex flex-col sm:flex-row sm:items-center">
																		<span class="text-muted-foreground w-full text-sm font-medium sm:w-16"
																			>{m.common_gateway()}:</span
																		>
																		<code
																			class="bg-muted text-muted-foreground cursor-pointer rounded px-1.5 py-0.5 font-mono text-xs break-all select-all sm:text-sm"
																			title="Click to select"
																		>
																			{cfg.gateway}
																		</code>
																	</div>
																{/if}
																{#if cfg.ipRange}
																	<div class="flex flex-col sm:flex-row sm:items-center">
																		<span class="text-muted-foreground w-full text-sm font-medium sm:w-16">Range:</span>
																		<code
																			class="bg-muted text-muted-foreground cursor-pointer rounded px-1.5 py-0.5 font-mono text-xs break-all select-all sm:text-sm"
																			title="Click to select"
																		>
																			{cfg.ipRange}
																		</code>
																	</div>
																{/if}
															</div>
														{/each}
													{/if}
													{#if info.configNetwork.ipv6Configs.length > 0}
														{#each info.configNetwork.ipv6Configs as cfg}
															<div class="bg-muted/30 space-y-1 rounded-lg p-2.5">
																<div class="text-muted-foreground mb-1 text-xs font-semibold">IPv6</div>
																<div class="flex flex-col sm:flex-row sm:items-center">
																	<span class="text-muted-foreground w-full text-sm font-medium sm:w-16"
																		>{m.common_subnet()}:</span
																	>
																	<code
																		class="bg-muted text-muted-foreground cursor-pointer rounded px-1.5 py-0.5 font-mono text-xs break-all select-all sm:text-sm"
																		title="Click to select"
																	>
																		{cfg.subnet}
																	</code>
																</div>
																{#if cfg.gateway}
																	<div class="flex flex-col sm:flex-row sm:items-center">
																		<span class="text-muted-foreground w-full text-sm font-medium sm:w-16"
																			>{m.common_gateway()}:</span
																		>
																		<code
																			class="bg-muted text-muted-foreground cursor-pointer rounded px-1.5 py-0.5 font-mono text-xs break-all select-all sm:text-sm"
																			title="Click to select"
																		>
																			{cfg.gateway}
																		</code>
																	</div>
																{/if}
																{#if cfg.ipRange}
																	<div class="flex flex-col sm:flex-row sm:items-center">
																		<span class="text-muted-foreground w-full text-sm font-medium sm:w-16">Range:</span>
																		<code
																			class="bg-muted text-muted-foreground cursor-pointer rounded px-1.5 py-0.5 font-mono text-xs break-all select-all sm:text-sm"
																			title="Click to select"
																		>
																			{cfg.ipRange}
																		</code>
																	</div>
																{/if}
															</div>
														{/each}
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
				<div class="text-muted-foreground rounded-lg border border-dashed py-12 text-center">
					<div class="text-sm">{m.containers_no_networks_connected()}</div>
				</div>
			{/if}
		</Card.Content>
	</Card.Root>
</div>
