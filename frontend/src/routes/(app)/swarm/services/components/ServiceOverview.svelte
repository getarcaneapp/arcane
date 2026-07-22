<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import { Badge } from '$lib/components/ui/badge';
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

<Card.Root>
	<Card.Header icon={InfoIcon}>
		<div class="flex flex-col space-y-1.5">
			<Card.Title>
				<h2>{m.common_overview()}</h2>
			</Card.Title>
			<Card.Description>{m.common_details_description({ resource: m.swarm_service() })}</Card.Description>
		</div>
	</Card.Header>
	<Card.Content class="p-4">
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
			<Card.Root variant="subtle">
				<Card.Content class="flex flex-col gap-2 p-4">
					<div class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
						{m.common_image()}
					</div>
					<div class="cursor-pointer font-mono text-sm font-medium break-all text-foreground select-all">
						{truncateImageDigest(serviceImage) || m.common_na()}
					</div>
				</Card.Content>
			</Card.Root>

			<Card.Root variant="subtle">
				<Card.Content class="flex flex-col gap-2 p-4">
					<div class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
						{m.common_version()}
					</div>
					<div class="font-mono text-sm font-medium text-foreground">
						{versionIndex}
					</div>
				</Card.Content>
			</Card.Root>

			<KeyValueCard label={m.common_id()}>{service.id}</KeyValueCard>

			<Card.Root variant="subtle">
				<Card.Content class="flex flex-col gap-2 p-4">
					<div class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
						{m.common_created()}
					</div>
					<div class="text-sm font-medium text-foreground">
						{formatRelative(service.createdAt)}
					</div>
					<div class="text-xs text-muted-foreground">
						{formatDate(service.createdAt)}
					</div>
				</Card.Content>
			</Card.Root>

			<Card.Root variant="subtle">
				<Card.Content class="flex flex-col gap-2 p-4">
					<div class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
						{m.common_updated()}
					</div>
					<div class="text-sm font-medium text-foreground">
						{formatRelative(service.updatedAt)}
					</div>
					<div class="text-xs text-muted-foreground">
						{formatDate(service.updatedAt)}
					</div>
				</Card.Content>
			</Card.Root>

			<Card.Root variant="subtle">
				<Card.Content class="flex flex-col gap-2 p-4">
					<div class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
						{m.nodes()}
					</div>
					{#if nodes.length > 0}
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
					{/if}
				</Card.Content>
			</Card.Root>

			{#if updateStatus?.['State']}
				<Card.Root variant="subtle" class="sm:col-span-2">
					<Card.Content class="flex flex-col gap-2 p-4">
						<div class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">{m.common_status()}</div>
						<div class="flex items-center gap-2">
							<Badge
								variant={updateStatus['State'] === 'completed'
									? 'green'
									: updateStatus['State'] === 'updating'
										? 'amber'
										: updateStatus['State'] === 'paused'
											? 'amber'
											: 'red'}
								minWidth="20">{updateStatus['State']}</Badge
							>
							{#if updateStatus['Message']}
								<span class="text-sm text-muted-foreground">{updateStatus['Message']}</span>
							{/if}
						</div>
						{#if updateStatus['CompletedAt']}
							<div class="text-xs text-muted-foreground">
								{formatRelative(updateStatus['CompletedAt'])}
							</div>
						{/if}
					</Card.Content>
				</Card.Root>
			{/if}
		</div>
	</Card.Content>
</Card.Root>
