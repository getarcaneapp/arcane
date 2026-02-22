<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { m } from '$lib/paraglide/messages';
	import { NetworksIcon } from '$lib/icons';

	interface Props {
		ports: any[];
		networks: any[];
		virtualIPs: any[];
	}

	let { ports, networks, virtualIPs }: Props = $props();

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
</script>

<div class="space-y-6">
	<Card.Root>
		<Card.Header icon={NetworksIcon}>
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
				<div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
					{#each networks as network}
						{@const networkId = network.Target || network.target || ''}
						{@const aliases = network.Aliases || network.aliases || []}
						{@const vip = vipMap.get(networkId)}
						<Card.Root variant="subtle">
							<Card.Content class="p-4">
								<div class="border-border mb-4 flex items-center gap-3 border-b pb-4">
									<div class="rounded-lg bg-blue-500/10 p-2">
										<NetworksIcon class="size-5 text-blue-500" />
									</div>
									<div class="min-w-0 flex-1">
										<div class="text-foreground text-base font-semibold break-all">
											{aliases.length > 0 ? aliases[0] : networkId.slice(0, 12)}
										</div>
										<div class="text-muted-foreground text-xs">Network</div>
									</div>
								</div>

								<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
									{#if vip}
										<Card.Root variant="outlined">
											<Card.Content class="flex flex-col p-3">
												<div class="text-muted-foreground mb-2 text-xs font-semibold">Virtual IP</div>
												<div
													class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all"
													title="Click to select"
												>
													{vip}
												</div>
											</Card.Content>
										</Card.Root>
									{/if}

									<Card.Root variant="outlined">
										<Card.Content class="flex flex-col p-3">
											<div class="text-muted-foreground mb-2 text-xs font-semibold">Network ID</div>
											<div
												class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all"
												title="Click to select"
											>
												{networkId}
											</div>
										</Card.Content>
									</Card.Root>

									{#if aliases.length > 0}
										<Card.Root variant="outlined" class="sm:col-span-2">
											<Card.Content class="flex flex-col p-3">
												<div class="text-muted-foreground mb-2 text-xs font-semibold">
													{m.containers_aliases()}
												</div>
												<div class="text-foreground space-y-1 text-sm font-medium">
													{#each aliases as alias}
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
