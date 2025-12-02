<script lang="ts">
	import * as Card from '$lib/components/ui/card/index.js';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import type { Component, Snippet } from 'svelte';
	import type { IconProps } from '@lucide/svelte';

	type Color = 'blue' | 'emerald' | 'purple';

	let {
		title,
		icon,
		color = 'blue',
		loading = false,
		rightBadge = null as string | number | null,
		subtitle = null as string | null,
		class: className,
		children
	}: {
		title: string;
		icon: Component<IconProps, {}, ''>;
		color?: Color;
		loading?: boolean;
		rightBadge?: string | number | null;
		subtitle?: string | null;
		class?: string;
		children?: Snippet;
	} = $props();

	const frameGradientClass = $derived(
		color === 'emerald'
			? 'before:[background:linear-gradient(135deg,rgba(16,185,129,0.35),rgba(16,185,129,0))]'
			: color === 'purple'
				? 'before:[background:linear-gradient(135deg,rgba(168,85,247,0.35),rgba(168,85,247,0))]'
				: 'before:[background:linear-gradient(135deg,rgba(59,130,246,0.35),rgba(59,130,246,0))]'
	);

	const iconBoxClass = $derived(
		color === 'emerald'
			? 'bg-emerald-500/10 ring-1 ring-emerald-500/30'
			: color === 'purple'
				? 'bg-purple-500/10 ring-1 ring-purple-500/30'
				: 'bg-blue-500/10 ring-1 ring-blue-500/30'
	);

	const iconColorClass = $derived(
		color === 'emerald' ? 'text-emerald-400' : color === 'purple' ? 'text-purple-400' : 'text-blue-400'
	);

	const glowBgClass = $derived(
		color === 'emerald' ? 'bg-emerald-500/20' : color === 'purple' ? 'bg-purple-500/20' : 'bg-blue-500/20'
	);
</script>

<Card.Root
	class={`bg-card/90 ring-border/40 group relative flex flex-col gap-6 overflow-hidden rounded-xl border py-3 shadow-sm ring-1 backdrop-blur-sm ring-inset
        before:pointer-events-none before:absolute before:inset-0 before:rounded-[inherit] before:[mask-composite:exclude] before:p-[1px]
        before:content-['']
        before:[-webkit-mask-composite:xor]
        before:[-webkit-mask:linear-gradient(#000_0_0)_content-box,linear-gradient(#000_0_0)]
        before:[mask:linear-gradient(#000_0_0)_content-box,linear-gradient(#000_0_0)]
        ${frameGradientClass} ${className ?? ''}`}
>
	<Card.Content class="p-4 px-6">
		<svelte:fragment>
			{@const IconComponent = icon}
			<div class="flex items-start gap-3">
				<div class="relative">
					<div class={`flex size-10 items-center justify-center rounded-lg ${iconBoxClass}`}>
						{#if loading}
							<Spinner />
						{:else}
							<IconComponent class={`size-5 ${iconColorClass}`} />
						{/if}
					</div>
					<div
						class={`pointer-events-none absolute inset-0 rounded-lg ${glowBgClass} opacity-0 blur-md transition-opacity group-hover:opacity-30`}
					></div>
				</div>

				<div class="min-w-0 flex-1">
					<div class="flex items-center justify-between gap-2">
						<p class="text-sm font-medium">{title}</p>
						{#if !loading && rightBadge !== null}
							<span class="text-muted-foreground rounded-full border px-2 py-0.5 text-[10px]">{rightBadge}</span>
						{/if}
					</div>

					{#if loading}
						<div class="mt-2 space-y-2">
							<div class="bg-muted h-4 w-24 animate-pulse rounded"></div>
							<div class="bg-muted h-3 w-32 animate-pulse rounded"></div>
						</div>
					{:else}
						{#if subtitle}
							<p class="text-muted-foreground mt-1 text-xs">{subtitle}</p>
						{/if}
						{@render children?.()}
					{/if}
				</div>
			</div>
		</svelte:fragment>
	</Card.Content>
</Card.Root>
