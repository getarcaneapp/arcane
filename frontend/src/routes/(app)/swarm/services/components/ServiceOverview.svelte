<script lang="ts">
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import DetailPanel from '$lib/components/resource-detail/detail-panel.svelte';
	import DetailSectionCard from '$lib/components/detail-section-card.svelte';
	import { m } from '$lib/paraglide/messages';
	import type { SwarmServiceInspect } from '$lib/types/swarm';
	import { formatDistanceToNow } from 'date-fns';
	import { InfoIcon, ConnectionIcon } from '$lib/icons';
	import { formatDateTimeShort, truncateImageDigest } from '$lib/utils/formatting';
	import { getSwarmServiceModeLabel, getSwarmServiceModeVariant, isSwarmServiceModeScalable } from '$lib/utils/docker';
	import { KeyValueCard } from '$lib/components/resource-detail';

	interface Props {
		service: SwarmServiceInspect;
		serviceName: string;
		serviceImage: string;
		serviceMode: string;
		desiredReplicas: number;
		labels: Record<string, string>;
	}

	let { service, serviceName, serviceImage, serviceMode, desiredReplicas, labels }: Props = $props();

	function formatDate(input: string | undefined | null): string {
		if (!input) return m.common_na();
		return formatDateTimeShort(input) || m.common_na();
	}

	function formatRelative(input: string | undefined | null): string {
		if (!input) return m.common_na();
		try {
			return formatDistanceToNow(new Date(input), { addSuffix: true });
		} catch {
			return m.common_na();
		}
	}

	const stackName = $derived(labels?.['com.docker.stack.namespace'] || '');
	const nodes = $derived((service?.nodes as string[]) || []);
	const versionIndex = $derived(service?.version?.index ?? service?.version?.Index ?? 0);
	const updateStatus = $derived(service?.updateStatus as Record<string, any> | null | undefined);
	const canScaleService = $derived(isSwarmServiceModeScalable(serviceMode));
</script>

<DetailPanel>
	<DetailSectionCard
		icon={InfoIcon}
		title={m.common_overview()}
		description={m.common_details_description({ resource: m.swarm_service() })}
	>
		<div class="mb-6 grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-3">
			<div>
				<div class="text-muted-foreground mb-2 text-xs font-semibold tracking-wide uppercase">
					{m.common_name()}
				</div>
				<div class="text-foreground cursor-pointer text-base font-semibold break-all select-all">
					{serviceName}
				</div>
			</div>

			<div>
				<div class="text-muted-foreground mb-2 text-xs font-semibold tracking-wide uppercase">
					{m.swarm_stack()}
				</div>
				<div class="text-foreground text-base font-semibold">
					{stackName || m.common_na()}
				</div>
			</div>

			<div>
				<div class="text-muted-foreground mb-2 text-xs font-semibold tracking-wide uppercase">
					{m.swarm_mode()} / {m.swarm_replicas()}
				</div>
				<div class="flex items-center gap-2">
					<StatusBadge variant={getSwarmServiceModeVariant(serviceMode)} text={getSwarmServiceModeLabel(serviceMode)} />
					{#if canScaleService}
						<span class="text-foreground font-mono text-sm">
							{desiredReplicas}
							{m.swarm_replicas()}
						</span>
					{/if}
				</div>
			</div>
		</div>

		<div class="grid grid-cols-1 gap-x-6 gap-y-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
			<div class="flex flex-col gap-1">
				<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
					{m.common_image()}
				</div>
				<div class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all">
					{truncateImageDigest(serviceImage) || m.common_na()}
				</div>
			</div>

			<div class="flex flex-col gap-1">
				<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
					{m.common_version()}
				</div>
				<div class="text-foreground font-mono text-sm font-medium">
					{versionIndex}
				</div>
			</div>

			<KeyValueCard label={m.common_id()}>{service.id}</KeyValueCard>

			<div class="flex flex-col gap-1">
				<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
					{m.common_created()}
				</div>
				<div class="text-foreground text-sm font-medium">
					{formatRelative(service.createdAt)}
				</div>
				<div class="text-muted-foreground text-xs">
					{formatDate(service.createdAt)}
				</div>
			</div>

			<div class="flex flex-col gap-1">
				<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
					{m.common_updated()}
				</div>
				<div class="text-foreground text-sm font-medium">
					{formatRelative(service.updatedAt)}
				</div>
				<div class="text-muted-foreground text-xs">
					{formatDate(service.updatedAt)}
				</div>
			</div>

			<div class="flex flex-col gap-1">
				<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
					{m.swarm_nodes_column()}
				</div>
				{#if nodes.length > 0}
					<div class="flex flex-wrap gap-1.5">
						{#each nodes as node (node)}
							<div class="flex items-center gap-1">
								<ConnectionIcon class="text-muted-foreground size-3" />
								<span class="text-foreground text-sm font-medium">{node}</span>
							</div>
						{/each}
					</div>
				{:else}
					<span class="text-muted-foreground text-sm">{m.common_na()}</span>
				{/if}
			</div>

			{#if updateStatus?.['State']}
				<div class="flex flex-col gap-1 sm:col-span-2">
					<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">{m.common_status()}</div>
					<div class="flex items-center gap-2">
						<StatusBadge
							variant={updateStatus['State'] === 'completed'
								? 'green'
								: updateStatus['State'] === 'updating'
									? 'amber'
									: updateStatus['State'] === 'paused'
										? 'amber'
										: 'red'}
							text={updateStatus['State']}
						/>
						{#if updateStatus['Message']}
							<span class="text-muted-foreground text-sm">{updateStatus['Message']}</span>
						{/if}
					</div>
					{#if updateStatus['CompletedAt']}
						<div class="text-muted-foreground text-xs">
							{formatRelative(updateStatus['CompletedAt'])}
						</div>
					{/if}
				</div>
			{/if}
		</div>
	</DetailSectionCard>
</DetailPanel>
