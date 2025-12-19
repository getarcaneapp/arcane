<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import { Badge } from '$lib/components/ui/badge';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import DockerInfoDialog from '$lib/components/dialogs/docker-info-dialog.svelte';
	import type { DockerInfo } from '$lib/types/docker-info.type';
	import { m } from '$lib/paraglide/messages';
	import { DockerBrandIcon, ImagesIcon, ContainersIcon, InfoIcon } from '$lib/icons';

	let {
		dockerInfo,
		containersRunning,
		containersStopped,
		totalContainers,
		totalImages,
		loading = false,
		class: className
	}: {
		dockerInfo: DockerInfo | null;
		containersRunning: number;
		containersStopped: number;
		totalContainers: number;
		totalImages: number;
		loading?: boolean;
		class?: string;
	} = $props();

	let dockerInfoDialogOpen = $state(false);
</script>

<Card.Root class={className}>
	{#snippet children()}
		<Card.Header icon={DockerBrandIcon} iconVariant="primary" class="items-center" {loading}>
			{#snippet children()}
				{#if loading}
					<div class="flex flex-1 flex-col gap-2">
						<Skeleton class="h-4 w-32" />
						<Skeleton class="h-3 w-48" />
					</div>
				{:else}
					<div class="flex flex-1 flex-col gap-1.5">
						<div class="flex items-center gap-2">
							<span class="text-foreground text-sm font-semibold"
								>{m.docker_engine_title({ engine: dockerInfo?.Name ?? 'Docker Engine' })}</span
							>
							<Badge variant="outline" class="text-xs">{dockerInfo?.ServerVersion ?? '-'}</Badge>
						</div>
						<div class="text-muted-foreground flex flex-wrap items-center gap-3 text-xs">
							<span class="flex items-center gap-1.5">
								<ContainersIcon class="size-3" />
								<span class="font-medium text-emerald-600">{containersRunning}</span>
								<span class="text-muted-foreground/70">/</span>
								<span>{totalContainers}</span>
							</span>
							<span class="text-muted-foreground/50">•</span>
							<span class="flex items-center gap-1.5">
								<ImagesIcon class="size-3" />
								<span>{totalImages}</span>
								<span class="text-muted-foreground/70">{m.images_title().toLowerCase()}</span>
							</span>
							<span class="text-muted-foreground/50">•</span>
							<span class="font-mono">
								{dockerInfo?.OperatingSystem ?? '-'} / {dockerInfo?.Architecture ?? '-'}
							</span>
						</div>
					</div>

					{#if dockerInfo}
						<ArcaneButton
							action="base"
							tone="ghost"
							size="icon"
							icon={InfoIcon}
							class="text-muted-foreground hover:text-foreground size-8 shrink-0"
							onclick={() => (dockerInfoDialogOpen = true)}
						/>
					{/if}
				{/if}
			{/snippet}
		</Card.Header>
	{/snippet}
</Card.Root>

<DockerInfoDialog bind:open={dockerInfoDialogOpen} {dockerInfo} />
