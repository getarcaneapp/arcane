<script lang="ts">
	import { mergeProps } from 'bits-ui';

	import * as ArcaneTooltip from '#lib/components/arcane-tooltip/index.js';
	import { badgeVariants } from '#lib/components/ui/badge/index.js';
	import { useEnvironmentSettings } from '#lib/hooks/use-environment-settings.svelte.js';
	import { ArrowRightIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { ContainerPort } from '#lib/types/container.js';
	import type { ServicePort } from '#lib/types/project.js';
	import { cn } from '#lib/utils.js';
	import { toPortHref } from '#lib/utils/navigation.js';

	let {
		ports = [] as PortBadgePort[],
		environmentId,
		collapsible = true,
		maxVisible = 3,
		hideExposed = false,
		wrap = true
	}: {
		ports?: PortBadgePort[];
		environmentId: string;
		collapsible?: boolean;
		maxVisible?: number;
		hideExposed?: boolean;
		wrap?: boolean;
	} = $props();

	let expanded = $state(false);

	const environmentSettings = useEnvironmentSettings(() => environmentId);
	const baseServerUrl = $derived(environmentSettings.current?.values.baseServerUrl ?? 'http://localhost');
	type PortBadgePort = ContainerPort | ServicePort;

	type NormalizedPort = {
		hostPort: string | null;
		containerPort: string;
		proto?: string;
		ips: string[];
		isPublished: boolean;
	};

	function isContainerPort(p: PortBadgePort): p is ContainerPort {
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
		return Array.from(Map.groupBy(list.map(normalize), portKey).values(), (group) =>
			group.reduce((merged, n) => ({ ...merged, ips: [...new Set([...merged.ips, ...n.ips])] }))
		).sort((a, b) => {
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
							class:
								'inline-flex h-6.5 shrink-0 items-stretch overflow-hidden rounded-xl border border-border font-mono text-xs font-medium transition-colors hover:border-sky/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50',
							href: toPortHref(p.hostPort!, baseServerUrl),
							target: '_blank',
							rel: 'noopener noreferrer'
						})}
						<a {...triggerProps}>
							<span class="flex items-center bg-sky/10 pr-2 pl-2.5 leading-none text-sky tabular-nums">{p.hostPort}</span>
							<span class="flex items-center pr-2.5 pl-0.5 leading-none text-muted-foreground tabular-nums">
								<ArrowRightIcon class="size-3.5 shrink-0 opacity-50" />
								<span>
									<span class="text-foreground">{p.containerPort}</span>{#if p.proto}<span class="lowercase opacity-70"
											>/{p.proto}</span
										>{/if}
								</span>
							</span>
						</a>
					{/snippet}
				</ArcaneTooltip.Trigger>
				<ArcaneTooltip.Content>
					{#each p.ips as ip (ip)}
						<p class="text-xs">
							{m.published()}: {ip.includes(':') ? `[${ip}]` : ip}:{p.hostPort} → {p.containerPort}{p.proto ? `/${p.proto}` : ''}
						</p>
					{/each}
					<p class="mt-1 text-xs text-muted-foreground">
						{m.port_link_base_server_url_hint({
							url: toPortHref(p.hostPort!, baseServerUrl)
						})}
					</p>
				</ArcaneTooltip.Content>
			</ArcaneTooltip.Root>
		{/each}
		{#each exposedOnly as p (portKey(p))}
			<ArcaneTooltip.Root>
				<ArcaneTooltip.Trigger>
					<span
						class="inline-flex h-6.5 shrink-0 items-center gap-1.5 rounded-xl border border-border px-2.5 font-mono text-xs font-medium text-muted-foreground tabular-nums"
					>
						<span
							>{p.containerPort}{#if p.proto}<span class="lowercase opacity-70">/{p.proto}</span>{/if}</span
						>
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
							class: cn(
								badgeVariants({ variant: 'gray', size: 'sm' }),
								'h-6.5 shrink-0 cursor-pointer px-2.5 text-xs hover:text-foreground'
							)
						})}
						<button {...triggerProps}>{expanded ? '−' : `+${hiddenCount}`}</button>
					{/snippet}
				</ArcaneTooltip.Trigger>
				<ArcaneTooltip.Content>
					<p class="text-xs">
						{expanded
							? m.containers_show_fewer_ports()
							: m.containers_show_more_ports({
									count: hiddenCount
								})}
					</p>
				</ArcaneTooltip.Content>
			</ArcaneTooltip.Root>
		{/if}
	</div>
{/if}
