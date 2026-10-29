<script lang="ts">
	import { KeyValueCard } from '#lib/components/resource-detail/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import { InfoIcon, ConnectionIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { SwarmServiceInspect } from '#lib/types/swarm.js';
	import {
		SWARM_STACK_LABEL,
		getSwarmServiceModeLabel,
		getSwarmServiceModeVariant,
		isSwarmServiceModeScalable
	} from '#lib/utils/docker.js';
	import { formatDateTimeShort, formatRelativeTime, truncateImageDigest } from '#lib/utils/formatting.js';

	interface Props {
		service: SwarmServiceInspect;
		serviceName: string;
		serviceImage: string;
		serviceMode: string;
		desiredReplicas: number;
		labels: Record<string, string>;
	}

	let { service, serviceName, serviceImage, serviceMode, desiredReplicas, labels }: Props = $props();

	const stackName = $derived(labels?.[SWARM_STACK_LABEL] || '');
	const nodes = $derived((service?.nodes as string[]) || []);
	const versionIndex = $derived(service?.version?.index ?? service?.version?.Index ?? 0);
	const updateStatus = $derived(service?.updateStatus as Record<string, any> | null | undefined);
	const updateStatusVariant = $derived.by(() => {
		switch (updateStatus?.['State']) {
			case 'completed':
				return 'green';
			case 'updating':
			case 'paused':
				return 'amber';
			default:
				return 'red';
		}
	});
	const canScaleService = $derived(isSwarmServiceModeScalable(serviceMode));
</script>

<Card.Root>
	<Card.Header icon={InfoIcon}>
		<div class="flex flex-col space-y-1.5">
			<Card.Title>
				<h2>{m.common_overview()}</h2>
			</Card.Title>
			<Card.Description>{m.common_details_description({ resource: m.swarm_service() })}</Card.Description>
		</div>
	</Card.Header>
	<Card.Content>
		<div class="mb-6 grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-3">
			<div>
				<div class="mb-2 text-xs font-semibold tracking-wide text-muted-foreground uppercase">
					{m.common_name()}
				</div>
				<div class="cursor-pointer text-base font-semibold break-all text-foreground select-all">
					{serviceName}
				</div>
			</div>

			<div>
				<div class="mb-2 text-xs font-semibold tracking-wide text-muted-foreground uppercase">
					{m.swarm_stack()}
				</div>
				<div class="text-base font-semibold text-foreground">
					{stackName || m.common_na()}
				</div>
			</div>

			<div>
				<div class="mb-2 text-xs font-semibold tracking-wide text-muted-foreground uppercase">
					{m.swarm_mode()} / {m.swarm_replicas()}
				</div>
				<div class="flex items-center gap-2">
					<Badge variant={getSwarmServiceModeVariant(serviceMode)} minWidth="20">{getSwarmServiceModeLabel(serviceMode)}</Badge>
					{#if canScaleService}
						<span class="font-mono text-sm text-foreground">
							{desiredReplicas}
							{m.swarm_replicas()}
						</span>
					{/if}
				</div>
			</div>
		</div>

		<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
			<KeyValueCard label={m.common_image()} valueFormat="custom" stacked
				><div class="cursor-pointer font-mono text-sm font-medium break-all text-foreground select-all">
					{truncateImageDigest(serviceImage) || m.common_na()}
				</div></KeyValueCard
			>

			<KeyValueCard label={m.common_version()} valueFormat="custom" stacked
				><div class="font-mono text-sm font-medium text-foreground">
					{versionIndex}
				</div></KeyValueCard
			>

			<KeyValueCard label={m.common_id()}>{service.id}</KeyValueCard>

			<KeyValueCard label={m.common_created()} valueFormat="custom" stacked
				><div class="text-sm font-medium text-foreground">
					{formatRelativeTime(service.createdAt) || m.common_na()}
				</div>
				<div class="text-xs text-muted-foreground">
					{formatDateTimeShort(service.createdAt) || m.common_na()}
				</div></KeyValueCard
			>

			<KeyValueCard label={m.common_updated()} valueFormat="custom" stacked
				><div class="text-sm font-medium text-foreground">
					{formatRelativeTime(service.updatedAt) || m.common_na()}
				</div>
				<div class="text-xs text-muted-foreground">
					{formatDateTimeShort(service.updatedAt) || m.common_na()}
				</div></KeyValueCard
			>

			<KeyValueCard label={m.nodes()} valueFormat="custom" stacked
				>{#if nodes.length > 0}
					<div class="flex flex-wrap gap-1.5">
						{#each nodes as node (node)}
							<div class="flex items-center gap-1">
								<ConnectionIcon class="size-3 text-muted-foreground" />
								<span class="text-sm font-medium text-foreground">{node}</span>
							</div>
						{/each}
					</div>
				{:else}
					<span class="text-sm text-muted-foreground">{m.common_na()}</span>
				{/if}</KeyValueCard
			>

			{#if updateStatus?.['State']}
				<Card.Root variant="subtle" class="sm:col-span-2">
					<Card.Content>
						<div class="flex flex-col gap-2">
							<div class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">{m.common_status()}</div>
							<div class="flex items-center gap-2">
								<Badge variant={updateStatusVariant} minWidth="20">{updateStatus['State']}</Badge>
								{#if updateStatus['Message']}
									<span class="text-sm text-muted-foreground">{updateStatus['Message']}</span>
								{/if}
							</div>
							{#if updateStatus['CompletedAt']}
								<div class="text-xs text-muted-foreground">
									{formatRelativeTime(String(updateStatus['CompletedAt'])) || m.common_na()}
								</div>
							{/if}
						</div>
					</Card.Content>
				</Card.Root>
			{/if}
		</div>
	</Card.Content>
</Card.Root>
