<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import { Progress } from '$lib/components/ui/progress/index.js';
	import { m } from '$lib/paraglide/messages';

	interface Props {
		title: string;
		description?: string;
		currentValue?: number;
		unit?: string;
		formatValue?: (value: number) => string;
		maxValue?: number;
		icon: any;
		loading?: boolean;
		showAbsoluteValues?: boolean;
		formatAbsoluteValue?: (value: number) => string;
	}

	let {
		title,
		description,
		currentValue,
		unit = '',
		formatValue = (v) => `${v.toFixed(1)}${unit}`,
		maxValue = 100,
		icon,
		loading = false,
		showAbsoluteValues = false,
		formatAbsoluteValue = (v) => v.toString()
	}: Props = $props();

	const percentage = $derived(currentValue !== undefined && !loading && maxValue > 0 ? (currentValue / maxValue) * 100 : 0);
</script>

<Card.Root class="flex h-full flex-col">
	{#snippet children()}
		<Card.Header {icon} iconVariant="primary" compact {loading}>
			{#snippet children()}
				<div class="min-w-0 flex-1">
					<div class="text-foreground text-sm font-semibold">{title}</div>
					{#if description}
						<div class="text-muted-foreground text-xs">{description}</div>
					{/if}
				</div>
			{/snippet}
		</Card.Header>

		<Card.Content class="flex flex-1 flex-col justify-end gap-3 p-3 sm:p-4">
			{#if loading}
				<div class="flex items-center gap-3">
					<div class="bg-muted h-2.5 flex-1 animate-pulse rounded-full"></div>
					<div class="bg-muted h-4 w-12 animate-pulse rounded"></div>
				</div>
				<div class="flex justify-between gap-4">
					<div class="bg-muted h-4 w-16 animate-pulse rounded"></div>
					<div class="bg-muted h-4 w-20 animate-pulse rounded"></div>
				</div>
			{:else}
				<div class="flex items-center gap-3">
					<Progress value={percentage} max={100} class="h-2.5 flex-1" />
					<span class="text-foreground min-w-12 text-right text-sm font-bold tabular-nums">
						{currentValue !== undefined ? `${formatValue(currentValue)}${unit}` : m.common_na()}
					</span>
				</div>

				<div class="flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
					<div class="flex items-center gap-2 whitespace-nowrap">
						<div class="bg-primary size-2 shrink-0 rounded-full"></div>
						<span class="text-muted-foreground text-xs">{m.dashboard_meter_usage()}</span>
						<span class="text-foreground text-sm font-semibold tabular-nums">{percentage.toFixed(1)}%</span>
					</div>
					{#if showAbsoluteValues && currentValue !== undefined && maxValue !== undefined && formatAbsoluteValue}
						<div class="flex items-center gap-2 whitespace-nowrap">
							<div class="bg-primary/20 size-2 shrink-0 rounded-full"></div>
							<span class="text-muted-foreground text-xs">{m.dashboard_meter_capacity()}</span>
							<span class="text-foreground text-sm font-semibold tabular-nums">
								{maxValue === 100 ? formatAbsoluteValue(0) : formatAbsoluteValue(maxValue)}
							</span>
						</div>
					{/if}
				</div>
			{/if}
		</Card.Content>
	{/snippet}
</Card.Root>
