<script lang="ts">
	import type { ContainerPorts } from '$lib/types/container.type';
	import { m } from '$lib/paraglide/messages';
	import * as ArcaneTooltip from '$lib/components/arcane-tooltip';
	import settingsStore from '$lib/stores/config-store';

	let { ports = [] as ContainerPorts[] } = $props<{
		ports?: ContainerPorts[];
	}>();

	const baseServerUrl = $derived($settingsStore?.baseServerUrl ?? 'http://localhost');

	type NormalizedPort = {
		hostPort: string | null;
		containerPort: string;
		proto?: string;
		ip?: string | null;
		isPublished: boolean;
	};

	function getPublicPort(p: ContainerPorts): string | null {
		const pub =
			(p as any).publicPort?.toString?.() ?? (p as any).hostPort?.toString?.() ?? (p as any).published?.toString?.() ?? null;
		return pub && pub !== '0' ? pub : null;
	}

	function getPrivatePort(p: ContainerPorts): string {
		return ((p as any).privatePort ?? (p as any).target ?? '?').toString();
	}

	function getProto(p: ContainerPorts): string | undefined {
		return (p as any).type ?? (p as any).protocol ?? undefined;
	}

	function normalize(p: ContainerPorts): NormalizedPort {
		const hostPort = getPublicPort(p);
		return {
			hostPort,
			containerPort: getPrivatePort(p),
			proto: getProto(p),
			ip: (p as any).ip ?? null,
			isPublished: hostPort !== null
		};
	}

	function uniquePorts(list: ContainerPorts[]): NormalizedPort[] {
		const map = new Map<string, NormalizedPort>();
		for (const p of list) {
			const n = normalize(p);
			const key = `${n.hostPort ?? ''}:${n.containerPort}/${n.proto ?? ''}`;
			if (!map.has(key)) map.set(key, n);
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

	function toHref(hostPort: string): string {
		try {
			const base = baseServerUrl || (typeof window !== 'undefined' ? window.location.origin : 'http://localhost');
			const url = new URL(base.startsWith('http') ? base : `http://${base}`);
			url.port = hostPort;
			return url.toString();
		} catch {
			return '#';
		}
	}

	const allPorts = $derived(uniquePorts(ports));
	const published = $derived(allPorts.filter((p) => p.isPublished));
	const exposedOnly = $derived(allPorts.filter((p) => !p.isPublished));
</script>

{#if allPorts.length === 0}
	<span class="text-muted-foreground text-xs">{m.containers_no_ports()}</span>
{:else}
	<div class="flex flex-wrap gap-1.5">
		{#each published as p, i (i)}
			<ArcaneTooltip.Root interactive>
				<ArcaneTooltip.Trigger>
					<a
						class="ring-offset-background focus-visible:ring-ring bg-background/70 dark:bg-sky-500/20 inline-flex items-center gap-1 rounded-lg border border-sky-700/20 dark:border-sky-400/40 px-2 py-1 text-[11px] dark:text-sky-100 shadow-sm transition-colors hover:border-sky-700/40 hover:bg-sky-500/10 dark:hover:border-sky-300/60 dark:hover:bg-sky-500/30 hover:shadow-md focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none"
						href={toHref(p.hostPort!)}
						target="_blank"
						rel="noopener noreferrer"
					>
						<span class="font-medium tabular-nums">{p.hostPort}:{p.containerPort}</span>
						{#if p.proto}
							<span class="text-muted-foreground uppercase">{p.proto}</span>
						{/if}
					</a>
				</ArcaneTooltip.Trigger>
				<ArcaneTooltip.Content>
					<p class="text-xs">
						Published: {p.ip ?? '0.0.0.0'}:{p.hostPort} → {p.containerPort}{p.proto ? `/${p.proto}` : ''}
					</p>
				</ArcaneTooltip.Content>
			</ArcaneTooltip.Root>
		{/each}
		{#each exposedOnly as p, i (i)}
			<ArcaneTooltip.Root>
				<ArcaneTooltip.Trigger>
					<span
						class="bg-background/50 dark:bg-slate-500/20 inline-flex items-center gap-1 rounded-lg border border-gray-600/30 dark:border-slate-400/40 px-2 py-1 text-[11px] text-gray-400 dark:text-slate-200 shadow-sm"
					>
						<span class="tabular-nums">{p.containerPort}</span>
						{#if p.proto}
							<span class="text-muted-foreground uppercase">{p.proto}</span>
						{/if}
					</span>
				</ArcaneTooltip.Trigger>
				<ArcaneTooltip.Content>
					<p class="text-xs">
						Exposed: {p.containerPort}{p.proto ? `/${p.proto}` : ''} (not published to host)
					</p>
				</ArcaneTooltip.Content>
			</ArcaneTooltip.Root>
		{/each}
	</div>
{/if}
