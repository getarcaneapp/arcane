<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import HardDriveIcon from '@lucide/svelte/icons/hard-drive';
	import NetworkIcon from '@lucide/svelte/icons/network';
	import InfoIcon from '@lucide/svelte/icons/info';
	import PlayIcon from '@lucide/svelte/icons/play';
	import StopCircleIcon from '@lucide/svelte/icons/stop-circle';
	import HeartPulseIcon from '@lucide/svelte/icons/heart-pulse';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { PortBadge } from '$lib/components/badges';
	import { m } from '$lib/paraglide/messages';
	import type { ContainerDetailsDto } from '$lib/types/container.type';
	import { format, formatDistanceToNow } from 'date-fns';

	interface Props {
		container: ContainerDetailsDto;
		primaryIpAddress: string;
		baseServerUrl: string;
	}

	let { container, primaryIpAddress, baseServerUrl }: Props = $props();

	function parseDockerDate(input: string | Date | undefined | null): Date | null {
		if (!input) return null;
		if (input instanceof Date) return isNaN(input.getTime()) ? null : input;

		const s = String(input).trim();
		if (!s || s.startsWith('0001-01-01')) return null;

		const m = s.match(/^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(\.\d+)?Z$/);
		let normalized = s;
		if (m) {
			const base = m[1];
			const frac = m[2] ? m[2].slice(1) : '';
			const ms = frac ? '.' + frac.slice(0, 3).padEnd(3, '0') : '';
			normalized = `${base}${ms}Z`;
		}

		const d = new Date(normalized);
		return isNaN(d.getTime()) ? null : d;
	}

	function formatDockerDate(input: string | Date | undefined | null, fmt = 'PP p'): string {
		const d = parseDockerDate(input);
		return d ? format(d, fmt) : 'N/A';
	}

	function formatRelativeDate(input: string | Date | undefined | null): string {
		const d = parseDockerDate(input);
		if (!d) return 'N/A';
		try {
			return formatDistanceToNow(d, { addSuffix: true });
		} catch {
			return 'N/A';
		}
	}

	function getUptime(input: string | Date | undefined | null): string {
		const d = parseDockerDate(input);
		if (!d) return 'N/A';
		try {
			return formatDistanceToNow(d, { addSuffix: false });
		} catch {
			return 'N/A';
		}
	}

	const restartPolicy = $derived(container.hostConfig?.restartPolicy || 'no');

	// Deduplicate and categorize ports
	const uniquePorts = $derived.by(() => {
		if (!container.ports?.length) return { published: 0, exposed: 0, total: 0 };

		const seen = new Set<string>();
		let published = 0;
		let exposed = 0;

		for (const p of container.ports) {
			const privatePort = (p as any).privatePort ?? (p as any).target ?? 0;
			const publicPort = (p as any).publicPort ?? (p as any).hostPort ?? (p as any).published ?? null;
			const proto = (p as any).type ?? (p as any).protocol ?? 'tcp';

			// Create unique key for deduplication
			const key = `${publicPort ?? ''}:${privatePort}/${proto}`;
			if (seen.has(key)) continue;
			seen.add(key);

			if (publicPort && publicPort !== 0) {
				published++;
			} else {
				exposed++;
			}
		}

		return { published, exposed, total: published + exposed };
	});

	const mountCount = $derived(container.mounts?.length || 0);
	const networkCount = $derived(container.networkSettings?.networks ? Object.keys(container.networkSettings.networks).length : 0);
</script>

<Card.Root>
	<Card.Header icon={InfoIcon}>
		<div class="flex flex-col space-y-1.5">
			<Card.Title>
				<h2>
					{m.common_details_title({ resource: m.resource_container_cap() })}
				</h2>
			</Card.Title>
			<Card.Description>{m.common_details_description({ resource: m.resource_container() })}</Card.Description>
		</div>
	</Card.Header>
	<Card.Content class="p-4">
		<div class="mb-6 grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-4">
			<div>
				<div class="text-muted-foreground mb-2 text-xs font-semibold tracking-wide uppercase">
					{m.common_image()}
				</div>
				<div class="flex items-center gap-3">
					<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-blue-500/10">
						<HardDriveIcon class="size-5 text-blue-500" />
					</div>
					<div class="text-foreground cursor-pointer text-base font-semibold break-all select-all" title="Click to select">
						{container.image || m.common_na()}
					</div>
				</div>
			</div>

			{#if container.state?.running}
				<div>
					<div class="text-muted-foreground mb-2 text-xs font-semibold tracking-wide uppercase">Uptime</div>
					<div class="flex items-center gap-3">
						<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-green-500/10">
							<PlayIcon class="size-5 text-green-500" />
						</div>
						<div class="text-foreground text-base font-semibold">
							{getUptime(container.state.startedAt)}
						</div>
					</div>
				</div>
			{:else}
				<div>
					<div class="text-muted-foreground mb-2 text-xs font-semibold tracking-wide uppercase">Status</div>
					<div class="flex items-center gap-3">
						<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-gray-500/10">
							<StopCircleIcon class="size-5 text-gray-500" />
						</div>
						<div class="text-foreground text-base font-semibold">
							{container.state?.status || 'Stopped'}
						</div>
					</div>
				</div>
			{/if}

			<div>
				<div class="text-muted-foreground mb-2 text-xs font-semibold tracking-wide uppercase">
					{m.containers_ip_address()}
				</div>
				<div class="flex items-center gap-3">
					<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-purple-500/10">
						<NetworkIcon class="size-5 text-purple-500" />
					</div>
					<div class="text-foreground cursor-pointer font-mono text-base font-semibold select-all" title="Click to select">
						{primaryIpAddress}
					</div>
				</div>
			</div>

			{#if container.state?.health}
				<div>
					<div class="text-muted-foreground mb-2 text-xs font-semibold tracking-wide uppercase">Health Status</div>
					<div class="flex items-center gap-3">
						<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-pink-500/10">
							<HeartPulseIcon class="size-5 text-pink-500" />
						</div>
						<StatusBadge
							variant={container.state.health.status === 'healthy'
								? 'green'
								: container.state.health.status === 'unhealthy'
									? 'red'
									: 'amber'}
							text={container.state.health.status}
							size="md"
						/>
					</div>
				</div>
			{/if}
		</div>

		<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
			<Card.Root variant="subtle">
				<Card.Content class="flex flex-col gap-2 p-4">
					<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
						{m.common_id()}
					</div>
					<div class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all" title="Click to select">
						{container.id}
					</div>
				</Card.Content>
			</Card.Root>

			<Card.Root variant="subtle">
				<Card.Content class="flex flex-col gap-2 p-4">
					<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
						{m.common_created()}
					</div>
					<div class="text-foreground text-sm font-medium">
						{formatRelativeDate(container?.created)}
					</div>
					<div class="text-muted-foreground text-xs">
						{formatDockerDate(container?.created)}
					</div>
				</Card.Content>
			</Card.Root>

			{#if container.state?.running}
				<Card.Root variant="subtle">
					<Card.Content class="flex flex-col gap-2 p-4">
						<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">Started</div>
						<div class="text-foreground text-sm font-medium">
							{formatRelativeDate(container.state.startedAt)}
						</div>
						<div class="text-muted-foreground text-xs">
							{formatDockerDate(container.state.startedAt)}
						</div>
					</Card.Content>
				</Card.Root>
			{:else if container.state?.finishedAt && !container.state.finishedAt.startsWith('0001')}
				<Card.Root variant="subtle">
					<Card.Content class="flex flex-col gap-2 p-4">
						<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">Finished</div>
						<div class="text-foreground text-sm font-medium">
							{formatRelativeDate(container.state.finishedAt)}
						</div>
						<div class="text-muted-foreground text-xs">
							{formatDockerDate(container.state.finishedAt)}
						</div>
					</Card.Content>
				</Card.Root>
			{/if}

			<Card.Root variant="subtle">
				<Card.Content class="flex flex-col gap-2 p-4">
					<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">Restart Policy</div>
					<div class="text-foreground text-sm font-medium capitalize">
						{restartPolicy}
					</div>
				</Card.Content>
			</Card.Root>

			<Card.Root variant="subtle">
				<Card.Content class="flex flex-col gap-2 p-4">
					<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">Ports</div>
					<div class="text-foreground text-sm font-medium">
						{#if uniquePorts.total === 0}
							No ports
						{:else if uniquePorts.published > 0 && uniquePorts.exposed > 0}
							{uniquePorts.published} published, {uniquePorts.exposed} exposed
						{:else if uniquePorts.published > 0}
							{uniquePorts.published} published
						{:else}
							{uniquePorts.exposed} exposed
						{/if}
					</div>
				</Card.Content>
			</Card.Root>

			<Card.Root variant="subtle">
				<Card.Content class="flex flex-col gap-2 p-4">
					<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">Volumes</div>
					<div class="text-foreground text-sm font-medium">
						{mountCount}
						{mountCount === 1 ? 'mount' : 'mounts'}
					</div>
				</Card.Content>
			</Card.Root>

			<Card.Root variant="subtle">
				<Card.Content class="flex flex-col gap-2 p-4">
					<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">Networks</div>
					<div class="text-foreground text-sm font-medium">
						{networkCount}
						{networkCount === 1 ? 'network' : 'networks'}
					</div>
				</Card.Content>
			</Card.Root>

			<Card.Root variant="subtle">
				<Card.Content class="flex flex-col gap-2 p-4">
					<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">Image ID</div>
					<div class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all" title="Click to select">
						{container.imageId}
					</div>
				</Card.Content>
			</Card.Root>

			{#if container.config?.workingDir}
				<Card.Root variant="subtle">
					<Card.Content class="flex flex-col gap-2 p-4">
						<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
							{m.common_working_directory()}
						</div>
						<div
							class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all"
							title="Click to select"
						>
							{container.config.workingDir}
						</div>
					</Card.Content>
				</Card.Root>
			{/if}

			{#if container.config?.user}
				<Card.Root variant="subtle">
					<Card.Content class="flex flex-col gap-2 p-4">
						<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">User</div>
						<div class="text-foreground cursor-pointer font-mono text-sm font-medium select-all" title="Click to select">
							{container.config.user}
						</div>
					</Card.Content>
				</Card.Root>
			{/if}

			{#if container.config?.entrypoint && container.config.entrypoint.length > 0}
				<Card.Root variant="subtle" class="sm:col-span-2">
					<Card.Content class="flex flex-col gap-2 p-4">
						<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">Entrypoint</div>
						<div
							class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all"
							title="Click to select"
						>
							{container.config.entrypoint.join(' ')}
						</div>
					</Card.Content>
				</Card.Root>
			{/if}

			{#if container.config?.cmd && container.config.cmd.length > 0}
				<Card.Root variant="subtle" class="sm:col-span-2 lg:col-span-3 xl:col-span-4">
					<Card.Content class="flex flex-col gap-2 p-4">
						<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
							{m.common_command()}
						</div>
						<div
							class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all"
							title="Click to select"
						>
							{container.config.cmd.join(' ')}
						</div>
					</Card.Content>
				</Card.Root>
			{/if}
		</div>

		{#if container.ports && container.ports.length > 0}
			<div class="mt-6">
				<div class="text-muted-foreground mb-3 text-xs font-semibold tracking-wide uppercase">
					{m.common_port_mappings()}
				</div>
				<PortBadge ports={container.ports} {baseServerUrl} />
			</div>
		{/if}
	</Card.Content>
</Card.Root>
