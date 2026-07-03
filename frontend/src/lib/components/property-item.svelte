<script lang="ts">
	import type { Snippet } from 'svelte';
	import { m } from '$lib/paraglide/messages';

	const colorClasses = {
		gray: { bg: 'bg-gray-500/10', text: 'text-gray-500' },
		blue: { bg: 'bg-blue-500/10', text: 'text-blue-500' },
		orange: { bg: 'bg-orange-500/10', text: 'text-orange-500' },
		purple: { bg: 'bg-purple-500/10', text: 'text-purple-500' },
		green: { bg: 'bg-green-500/10', text: 'text-green-500' },
		yellow: { bg: 'bg-yellow-500/10', text: 'text-yellow-500' },
		red: { bg: 'bg-red-500/10', text: 'text-red-500' },
		indigo: { bg: 'bg-indigo-500/10', text: 'text-indigo-500' },
		cyan: { bg: 'bg-cyan-500/10', text: 'text-cyan-500' },
		pink: { bg: 'bg-pink-500/10', text: 'text-pink-500' },
		amber: { bg: 'bg-amber-500/10', text: 'text-amber-500' },
		teal: { bg: 'bg-teal-500/10', text: 'text-teal-500' }
	} as const;

	interface Props {
		icon: any;
		color: keyof typeof colorClasses;
		label: string;
		value?: string;
		valueClass?: string;
		class?: string;
		children?: Snippet;
	}

	let {
		icon: Icon,
		color,
		label,
		value,
		valueClass = 'mt-1 cursor-pointer text-sm font-semibold select-all sm:text-base',
		class: className = 'flex items-start gap-3',
		children
	}: Props = $props();
</script>

<div class={className}>
	<div class="flex size-10 shrink-0 items-center justify-center rounded-full {colorClasses[color].bg} p-2">
		<Icon class="size-5 {colorClasses[color].text}" />
	</div>
	<div class="min-w-0 flex-1">
		<p class="text-muted-foreground text-sm font-medium">{label}</p>
		{#if children}
			{@render children()}
		{:else}
			<p class={valueClass} title={m.common_click_to_select()}>
				{value}
			</p>
		{/if}
	</div>
</div>
