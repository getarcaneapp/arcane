<script lang="ts">
	import type { ResolvedPathname } from '$app/types';
	import type { Snippet } from 'svelte';

	import * as ArcaneTooltip from '#lib/components/arcane-tooltip/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { InfoIcon, type IconType } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';

	type SegmentTone = 'success' | 'warning' | 'destructive' | 'info' | 'muted';

	const segmentToneClass: Record<SegmentTone, string> = {
		success: 'bg-success',
		warning: 'bg-warning',
		destructive: 'bg-destructive',
		info: 'bg-info',
		muted: 'bg-muted-foreground/40'
	};

	interface Props {
		label: string;
		value: string | number | null;
		unavailableLabel?: string;
		icon?: IconType;
		help?: string;
		href?: ResolvedPathname;
		loading?: boolean;
		segments?: { value: number; tone: SegmentTone }[];
		children?: Snippet;
	}

	let {
		label,
		value,
		unavailableLabel = m.common_unavailable(),
		icon: Icon,
		help,
		href,
		loading = false,
		segments = [],
		children
	}: Props = $props();

	const visibleSegments = $derived(segments.filter((segment) => segment.value > 0));
</script>

{#snippet labelContent()}
	{#if Icon}
		<Icon class="size-3.5 shrink-0" />
	{/if}
	{label}
{/snippet}

<div class="group relative flex min-w-0 flex-col items-center px-4 text-center">
	<dt
		class="flex items-center justify-center gap-1.5 text-sm text-muted-foreground transition-colors group-hover:text-foreground"
	>
		{#if href}
			<a
				{href}
				class="flex items-center gap-1.5 after:absolute after:inset-0 after:rounded-sm focus-visible:outline-none focus-visible:after:outline-2 focus-visible:after:outline-offset-4 focus-visible:after:outline-ring"
			>
				{@render labelContent()}
			</a>
		{:else}
			{@render labelContent()}
		{/if}
		{#if help}
			<ArcaneTooltip.Root>
				<ArcaneTooltip.Trigger class="relative z-10">
					<span class="inline-flex cursor-help items-center" aria-label={label}>
						<InfoIcon class="size-3.5 shrink-0" />
					</span>
				</ArcaneTooltip.Trigger>
				<ArcaneTooltip.Content class="max-w-64">
					<p class="mb-1 text-sm font-medium">{label}</p>
					<p class="text-xs">{help}</p>
				</ArcaneTooltip.Content>
			</ArcaneTooltip.Root>
		{/if}
	</dt>
	<dd class="mt-2 flex flex-col items-center gap-1">
		{#if loading}
			<Skeleton class="h-9 w-14" />
			<Skeleton class="h-3.5 w-24" />
		{:else}
			{#if value === null}
				<span class="text-3xl font-semibold text-muted-foreground" aria-label={unavailableLabel}>–</span>
			{:else}
				<span class="text-3xl font-semibold tabular-nums">{value}</span>
			{/if}
			{#if visibleSegments.length > 0}
				<div class="my-1 flex h-1 w-24 gap-0.5 overflow-hidden rounded-full bg-muted" aria-hidden="true">
					{#each visibleSegments as segment, index (index)}
						<span class={['h-full grow-(--segment)', segmentToneClass[segment.tone]]} style:--segment={segment.value}></span>
					{/each}
				</div>
			{/if}
			{#if children}
				<div class="text-xs text-muted-foreground tabular-nums">
					{@render children()}
				</div>
			{/if}
		{/if}
	</dd>
</div>
