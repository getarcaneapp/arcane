<script lang="ts">
	import type { ContainerPorts } from '#lib/types/docker.js';
	import type { ServicePort } from '#lib/types/swarm.js';
	import { m } from '#lib/paraglide/messages.js';
	import * as ArcaneTooltip from '#lib/components/arcane-tooltip/index.js';
	import { badgeVariants } from '#lib/components/ui/badge/index.js';
	import { cn } from '#lib/utils.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import { toPortHref } from '#lib/utils/navigation.js';
	import { mergeProps } from 'bits-ui';

	let {
		ports = [] as PortBadgePort[],
		collapsible = true,
		maxVisible = 3,
		hideExposed = false,
		wrap = true
	} = $props<{
		ports?: PortBadgePort[];
		collapsible?: boolean;
		maxVisible?: number;
		hideExposed?: boolean;
		// In tables, set false so badges stay on one line (wrapping grows the row height).
		wrap?: boolean;
	}>();

	let expanded = $state(false);

	const baseServerUrl = $derived(settingsStore.current?.baseServerUrl ?? 'http://localhost');
	type PortBadgePort = ContainerPorts | ServicePort;

	type NormalizedPort = {
		hostPort: string | null;
		containerPort: string;
		proto?: string;
		ips: string[];
		isPublished: boolean;
	};

	function isContainerPort(p: PortBadgePort): p is ContainerPorts {
		return 'privatePort' in p;
	}

	function getPublicPort(p: PortBadgePort): string | null {
		const pub = isContainerPort(p) ? p.publicPort?.toString() : p.published?.toString();
		return pub && pub !== '0' ? pub : null;
	}

	function getPrivatePort(p: PortBadgePort): string {
		return (isContainerPort(p) ? p.privatePort : p.target).toString();
	}

	function getProto(p: PortBadgePort): string | undefined {
		return isContainerPort(p) ? p.type : p.protocol;
	}

	function normalize(p: PortBadgePort): NormalizedPort {
		const hostPort = getPublicPort(p);
		// An unspecified host IP binds all interfaces.
		const ip = (isContainerPort(p) ? p.ip : p.host_ip)?.trim() || '0.0.0.0';
		return {
			hostPort,
			containerPort: getPrivatePort(p),
			proto: getProto(p),
			ips: [ip],
			isPublished: hostPort !== null
		};
	}

	function portKey(p: NormalizedPort): string {
		return `${p.hostPort ?? ''}:${p.containerPort}/${p.proto ?? ''}`;
	}

	// Bindings that differ only by host IP (e.g. 0.0.0.0 and ::) share one badge.
	function uniquePorts(list: PortBadgePort[]): NormalizedPort[] {
		const map = new Map<string, NormalizedPort>();
		for (const p of list) {
			const n = normalize(p);
			const existing = map.get(portKey(n));
			if (!existing) {
				map.set(portKey(n), n);
				continue;
			}
			for (const ip of n.ips) if (!existing.ips.includes(ip)) existing.ips.push(ip);
		}
		return Array.from(map.values()).sort((a, b) => {
			// Published ports first
			if (a.isPublished !== b.isPublished) {
				return a.isPublished ? -1 : 1;
			}
			const hp = Number(a.hostPort ?? 0) - Number(b.hostPort ?? 0);
			if (hp !== 0) return hp;
			return Number(a.containerPort) - Number(b.containerPort);
		});
	}

	const allPorts = $derived(uniquePorts(ports));
	const visiblePorts = $derived(
		collapsible && !expanded && allPorts.length > maxVisible ? allPorts.slice(0, maxVisible) : allPorts
	);
	const published = $derived(visiblePorts.filter((p) => p.isPublished));
	const exposedOnly = $derived(hideExposed ? [] : visiblePorts.filter((p) => !p.isPublished));
	const hiddenCount = $derived(
		hideExposed ? allPorts.filter((p) => p.isPublished).length - published.length : allPorts.length - visiblePorts.length
	);
</script>

{#if allPorts.length === 0}
	<span class="text-xs text-muted-foreground">{m.containers_no_ports()}</span>
{:else}
	<div class="flex gap-1.5 {wrap ? 'flex-wrap' : 'flex-nowrap'}">
		{#each published as p (portKey(p))}
			<ArcaneTooltip.Root interactive>
				<ArcaneTooltip.Trigger>
					{#snippet child({ props })}
						{@const triggerProps = mergeProps(props, {
							class: cn(
								badgeVariants({ variant: 'sky', size: 'sm' }),
								'hover:bg-info/20 hover:border-info/40 focus-visible:outline-none'
							),
							href: toPortHref(p.hostPort!, baseServerUrl),
							target: '_blank',
							rel: 'noopener noreferrer'
						})}
						<a {...triggerProps}>
							<span class="font-medium tabular-nums">{p.hostPort}:{p.containerPort}</span>
							{#if p.proto}
								<span class="text-muted-foreground uppercase">{p.proto}</span>
							{/if}
						</a>
					{/snippet}
				</ArcaneTooltip.Trigger>
				<ArcaneTooltip.Content>
					{#each p.ips as ip (ip)}
						<p class="text-xs">
							{m.published()}: {ip.includes(':') ? `[${ip}]` : ip}:{p.hostPort} → {p.containerPort}{p.proto ? `/${p.proto}` : ''}
						</p>
					{/each}
				</ArcaneTooltip.Content>
			</ArcaneTooltip.Root>
		{/each}
		{#each exposedOnly as p (portKey(p))}
			<ArcaneTooltip.Root>
				<ArcaneTooltip.Trigger>
					<span class={badgeVariants({ variant: 'gray', size: 'sm' })}>
						<span class="tabular-nums">{p.containerPort}</span>
						{#if p.proto}
							<span class="text-muted-foreground uppercase">{p.proto}</span>
						{/if}
					</span>
				</ArcaneTooltip.Trigger>
				<ArcaneTooltip.Content>
					<p class="text-xs">
						{m.ports_exposed_label()}: {p.containerPort}{p.proto ? `/${p.proto}` : ''} ({m.ports_no_host_binding()})
					</p>
				</ArcaneTooltip.Content>
			</ArcaneTooltip.Root>
		{/each}
		{#if collapsible && allPorts.length > maxVisible}
			<ArcaneTooltip.Root>
				<ArcaneTooltip.Trigger>
					{#snippet child({ props })}
						{@const triggerProps = mergeProps(props, {
							onclick: () => (expanded = !expanded),
							class: cn(badgeVariants({ variant: 'gray', size: 'sm' }), 'hover:bg-muted cursor-pointer')
						})}
						<button {...triggerProps}>{expanded ? '−' : `+${hiddenCount}`}</button>
					{/snippet}
				</ArcaneTooltip.Trigger>
				<ArcaneTooltip.Content>
					<p class="text-xs">
						{expanded ? m.containers_show_fewer_ports() : m.containers_show_more_ports({ count: hiddenCount })}
					</p>
				</ArcaneTooltip.Content>
			</ArcaneTooltip.Root>
		{/if}
	</div>
{/if}
