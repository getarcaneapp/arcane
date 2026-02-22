<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { m } from '$lib/paraglide/messages';
	import type { SwarmServiceInspect } from '$lib/types/swarm.type';
	import { format, formatDistanceToNow } from 'date-fns';
	import { InfoIcon } from '$lib/icons';
	import { truncateImageDigest } from '$lib/utils/string.utils';

	interface Props {
		service: SwarmServiceInspect;
		serviceName: string;
		serviceImage: string;
		serviceMode: string;
		desiredReplicas: number;
		labels: Record<string, string>;
	}

	let { service, serviceName, serviceImage, serviceMode, desiredReplicas, labels }: Props = $props();

	function formatDate(input: string | undefined | null, fmt = 'PP p'): string {
		if (!input) return 'N/A';
		try {
			return format(new Date(input), fmt);
		} catch {
			return 'N/A';
		}
	}

	function formatRelative(input: string | undefined | null): string {
		if (!input) return 'N/A';
		try {
			return formatDistanceToNow(new Date(input), { addSuffix: true });
		} catch {
			return 'N/A';
		}
	}

	const stackName = $derived(labels?.['com.docker.stack.namespace'] || '');
	const versionIndex = $derived(service?.version?.index ?? service?.version?.Index ?? 0);
	const updateStatus = $derived(service?.updateStatus as Record<string, any> | null | undefined);
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
		<div class="mb-6 grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-4">
			<div>
				<div class="text-muted-foreground mb-2 text-xs font-semibold tracking-wide uppercase">
					{m.common_image()}
				</div>
				<div class="text-foreground cursor-pointer text-base font-semibold break-all select-all" title="Click to select">
					{truncateImageDigest(serviceImage) || m.common_na()}
				</div>
			</div>

			<div>
				<div class="text-muted-foreground mb-2 text-xs font-semibold tracking-wide uppercase">
					{m.swarm_mode()}
				</div>
				<div class="flex items-center gap-2">
					<StatusBadge
						variant={serviceMode === 'replicated' ? 'blue' : serviceMode === 'global' ? 'green' : 'gray'}
						text={serviceMode}
					/>
					{#if serviceMode === 'replicated'}
						<span class="text-foreground font-mono text-sm">
							{desiredReplicas} replica{desiredReplicas !== 1 ? 's' : ''}
						</span>
					{/if}
				</div>
			</div>

			<div>
				<div class="text-muted-foreground mb-2 text-xs font-semibold tracking-wide uppercase">
					{m.common_version()}
				</div>
				<div class="text-foreground font-mono text-base font-semibold">
					{versionIndex}
				</div>
			</div>

			{#if stackName}
				<div>
					<div class="text-muted-foreground mb-2 text-xs font-semibold tracking-wide uppercase">
						{m.swarm_stack()}
					</div>
					<div class="text-foreground text-base font-semibold">
						{stackName}
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
						{service.id}
					</div>
				</Card.Content>
			</Card.Root>

			<Card.Root variant="subtle">
				<Card.Content class="flex flex-col gap-2 p-4">
					<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
						{m.common_created()}
					</div>
					<div class="text-foreground text-sm font-medium">
						{formatRelative(service.createdAt)}
					</div>
					<div class="text-muted-foreground text-xs">
						{formatDate(service.createdAt)}
					</div>
				</Card.Content>
			</Card.Root>

			<Card.Root variant="subtle">
				<Card.Content class="flex flex-col gap-2 p-4">
					<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
						{m.common_updated()}
					</div>
					<div class="text-foreground text-sm font-medium">
						{formatRelative(service.updatedAt)}
					</div>
					<div class="text-muted-foreground text-xs">
						{formatDate(service.updatedAt)}
					</div>
				</Card.Content>
			</Card.Root>

			<Card.Root variant="subtle">
				<Card.Content class="flex flex-col gap-2 p-4">
					<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
						{m.common_name()}
					</div>
					<div class="text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all" title="Click to select">
						{serviceName}
					</div>
				</Card.Content>
			</Card.Root>

			{#if updateStatus?.State}
				<Card.Root variant="subtle" class="sm:col-span-2">
					<Card.Content class="flex flex-col gap-2 p-4">
						<div class="text-muted-foreground text-xs font-semibold tracking-wide uppercase">Update Status</div>
						<div class="flex items-center gap-2">
							<StatusBadge
								variant={updateStatus.State === 'completed'
									? 'green'
									: updateStatus.State === 'updating'
										? 'amber'
										: updateStatus.State === 'paused'
											? 'amber'
											: 'red'}
								text={updateStatus.State}
							/>
							{#if updateStatus.Message}
								<span class="text-muted-foreground text-sm">{updateStatus.Message}</span>
							{/if}
						</div>
						{#if updateStatus.CompletedAt}
							<div class="text-muted-foreground text-xs">
								{formatRelative(updateStatus.CompletedAt)}
							</div>
						{/if}
					</Card.Content>
				</Card.Root>
			{/if}
		</div>
	</Card.Content>
</Card.Root>
