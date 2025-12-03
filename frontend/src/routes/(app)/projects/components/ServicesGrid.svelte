<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import LayersIcon from '@lucide/svelte/icons/layers';
	import HeartPulseIcon from '@lucide/svelte/icons/heart-pulse';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { getStatusVariant } from '$lib/utils/status.utils';
	import { capitalizeFirstLetter } from '$lib/utils/string.utils';
	import { m } from '$lib/paraglide/messages';
	import { Badge } from '$lib/components/ui/badge';
	import type { RuntimeService } from '$lib/types/project.type';

	interface Props {
		services?: RuntimeService[];
		projectId?: string;
	}

	let { services, projectId }: Props = $props();

	function getHealthColor(health: string | undefined): string {
		if (!health) return 'text-amber-500';
		const normalized = health.toLowerCase();
		if (normalized === 'healthy') return 'text-green-500';
		if (normalized === 'unhealthy') return 'text-red-500';
		return 'text-amber-500';
	}
</script>

{#snippet serviceCard(service: RuntimeService)}
	{@const status = service.status || 'unknown'}
	{@const containerUrl = projectId
		? `/containers/${service.containerId}?from=project&projectId=${projectId}`
		: `/containers/${service.containerId}`}

	{#if service.containerId}
		<a href={containerUrl} class="group">
			<Card.Root
				variant="subtle"
				class="group-hover:border-border/60 group-hover:bg-muted/50 flex h-full cursor-pointer transition-all duration-200"
			>
				<Card.Content class="flex flex-col p-4">
					<div class="flex items-start gap-3">
						<div class="rounded-lg bg-blue-500/10 p-2 transition-colors group-hover:bg-blue-500/15">
							<LayersIcon class="size-5 text-blue-500" />
						</div>
						<div class="min-w-0 flex-1">
							<div class="mb-2 flex items-center gap-2">
								<h3 class="text-foreground text-base font-semibold transition-colors">
									{service.containerName || service.name}
								</h3>
								<Badge variant="outline" class="text-xs">
									{service.name}
								</Badge>
							</div>
							<div class="flex flex-wrap items-center gap-3">
								<StatusBadge variant={getStatusVariant(status)} text={capitalizeFirstLetter(status)} />
								{#if service.health}
									{@const healthColor = getHealthColor(service.health)}
									<div class="flex items-center gap-1.5">
										<HeartPulseIcon class="{healthColor} size-4" />
										<span class="text-muted-foreground text-xs">{capitalizeFirstLetter(service.health)}</span>
									</div>
								{/if}
							</div>
							{#if service.ports && service.ports.length > 0}
								<div class="text-muted-foreground mt-2 text-xs">
									{m.common_ports()}: {service.ports.join(', ')}
								</div>
							{/if}
						</div>
					</div>
				</Card.Content>
			</Card.Root>
		</a>
	{:else}
		<Card.Root variant="subtle" class="flex h-full opacity-60">
			<Card.Content class="flex flex-col p-4">
				<div class="flex items-start gap-3">
					<div class="rounded-lg bg-amber-500/10 p-2">
						<LayersIcon class="size-5 text-amber-500" />
					</div>
					<div class="min-w-0 flex-1">
						<h3 class="text-foreground mb-2 text-base font-semibold">
							{service.name}
						</h3>
						<StatusBadge variant={getStatusVariant(status)} text={capitalizeFirstLetter(status)} />
						<p class="text-muted-foreground mt-2 text-xs">
							{m.compose_service_not_created()}
						</p>
					</div>
				</div>
			</Card.Content>
		</Card.Root>
	{/if}
{/snippet}

<Card.Root>
	<Card.Header icon={LayersIcon}>
		<div class="flex flex-col space-y-1.5">
			<Card.Title>
				<h2>
					{m.compose_services()}
				</h2>
			</Card.Title>
			<Card.Description>{m.compose_services_description()}</Card.Description>
		</div>
	</Card.Header>
	<Card.Content class="p-4">
		{#if services && services.length > 0}
			<div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
				{#each services as service, i (service.containerId || service.name || i)}
					{@render serviceCard(service)}
				{/each}
			</div>
		{:else}
			<div class="rounded-lg border border-dashed py-12 text-center">
				<div class="bg-muted/50 mx-auto mb-4 flex size-16 items-center justify-center rounded-full">
					<LayersIcon class="text-muted-foreground size-6" />
				</div>
				<div class="text-muted-foreground text-sm">{m.compose_no_services_found()}</div>
			</div>
		{/if}
	</Card.Content>
</Card.Root>
