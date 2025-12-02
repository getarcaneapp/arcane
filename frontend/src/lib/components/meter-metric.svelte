<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import { Progress } from '$lib/components/ui/progress/index.js';
	import type { Icon as IconType } from '@lucide/svelte';
	import { m } from '$lib/paraglide/messages';

	interface Props {
		title: string;
		description?: string;
		currentValue?: number;
		unit?: string;
		formatValue?: (value: number) => string;
		maxValue?: number;
		icon: typeof IconType;
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
	const Icon = $derived(icon);
</script>

<Card.Root>
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

		<Card.Content class="flex flex-col justify-center p-3">
			<div class="w-full space-y-2">
				<div class="text-center">
					{#if loading}
						<div class="bg-muted mx-auto h-7 w-14 animate-pulse rounded"></div>
					{:else}
						<div class="text-foreground text-lg font-bold">
							{currentValue !== undefined ? formatValue(currentValue) : m.common_na()}
						</div>
					{/if}
				</div>

				<div class="space-y-1.5">
					{#if loading}
						<div class="bg-muted h-1.5 w-full animate-pulse rounded"></div>
					{:else}
						<Progress value={percentage} max={100} class="h-1.5" />
					{/if}

					<div class="flex items-center justify-between text-xs">
						{#if loading}
							<div class="bg-muted h-3 w-12 animate-pulse rounded"></div>
							{#if showAbsoluteValues}
								<div class="bg-muted h-3 w-20 animate-pulse rounded"></div>
							{/if}
						{:else}
							<span class="text-muted-foreground font-medium">
								{percentage.toFixed(1)}%
							</span>
							{#if showAbsoluteValues}
								<span class="text-muted-foreground/70 font-mono text-[10px]">
									{#if currentValue !== undefined && maxValue !== undefined && formatAbsoluteValue}
										{#if maxValue === 100}
											{formatAbsoluteValue(0)}
										{:else}
											{formatAbsoluteValue(currentValue)} / {formatAbsoluteValue(maxValue)}
										{/if}
									{/if}
								</span>
							{/if}
						{/if}
					</div>
				</div>
			</div>
		</Card.Content>
	{/snippet}
</Card.Root>
