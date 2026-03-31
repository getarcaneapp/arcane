<script lang="ts">
	import * as ArcaneTooltip from '$lib/components/arcane-tooltip';
	import type { IconType } from '$lib/icons';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import { cn } from '$lib/utils';

	interface Props {
		icon: IconType;
		label: string;
		value: string;
		detail: string;
		history: Array<{ percent: number; tooltip: string }>;
		tone?: 'cpu' | 'memory';
		loading?: boolean;
		disabled?: boolean;
		testId?: string;
	}

	let { icon, label, value, detail, history, tone = 'cpu', loading = false, disabled = false, testId }: Props = $props();

	const barColorClass = $derived(tone === 'cpu' ? 'bg-sky-400/75' : 'bg-emerald-400/75');
	const iconColorClass = $derived(tone === 'cpu' ? 'text-sky-300' : 'text-emerald-300');
	const Icon = $derived(icon);
</script>

<section
	class={cn(
		'bg-card/35 border-border/70 grid min-w-0 gap-2 rounded-lg border px-3 py-2.5',
		disabled && 'bg-muted/25 text-muted-foreground'
	)}
	data-testid={testId}
>
	<div class="grid grid-cols-[minmax(0,1fr)_auto] items-start gap-x-3">
		<div class="flex min-w-0 items-center gap-2">
			<div class="bg-background/70 flex size-7 shrink-0 items-center justify-center rounded-md border border-white/8">
				<Icon class={cn('size-4', disabled ? 'text-muted-foreground' : iconColorClass)} />
			</div>
			<div class="min-w-0">
				<div class="text-[11px] font-medium tracking-normal text-white/70">{label}</div>
				{#if loading}
					<Skeleton class="mt-1 h-5 w-20" />
				{:else}
					<div class={cn('truncate text-sm font-semibold tabular-nums', disabled ? 'text-muted-foreground' : 'text-white')}>
						{value}
					</div>
				{/if}
			</div>
		</div>

		{#if loading}
			<Skeleton class="mt-0.5 h-4 w-16 justify-self-end" />
		{:else}
			<div class="shrink-0 justify-self-end pl-2 text-right text-[11px] text-white/55 tabular-nums">{detail}</div>
		{/if}
	</div>

	{#if loading}
		<Skeleton class="h-8 w-full" />
	{:else}
		<div class="bg-background/60 flex h-8 items-end gap-[3px] overflow-hidden rounded-md border border-white/8 px-1.5 py-1">
			{#each history as sample, index (`${tone}-${index}`)}
				<ArcaneTooltip.Root>
					<ArcaneTooltip.Trigger class="flex h-full min-w-0 flex-1 items-end self-stretch">
						<div
							class={cn('min-w-0 flex-1 rounded-[2px] transition-opacity', disabled ? 'bg-white/8' : barColorClass)}
							style={`height: ${Math.max(disabled ? 12 : Math.min(Math.max(sample.percent, 6), 100), 6)}%; opacity: ${disabled ? 0.35 : 0.45 + Math.min(sample.percent, 100) / 180};`}
						></div>
					</ArcaneTooltip.Trigger>
					<ArcaneTooltip.Content side="top">
						<div class="space-y-1">
							<div class="text-xs font-medium">{label}</div>
							<div class="text-muted-foreground text-xs tabular-nums">{sample.tooltip}</div>
						</div>
					</ArcaneTooltip.Content>
				</ArcaneTooltip.Root>
			{/each}
		</div>
	{/if}
</section>
