<script lang="ts">
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import { cn } from '$lib/utils.js';
	import type { TabItem } from './types.ts';

	interface Props {
		items: TabItem[];
		value: string;
		onValueChange: (value: string) => void;
		class?: string;
	}

	let { items, onValueChange, class: className }: Props = $props();
</script>

<Tabs.List class={cn('scrollbar-hide flex w-full justify-start gap-4 overflow-x-auto', className)}>
	{#each items as item}
		{@const IconComponent = item.icon}
		<Tabs.Trigger
			value={item.value}
			class={cn('flex-shrink-0 gap-2 whitespace-nowrap', item.class)}
			disabled={item.disabled}
			onclick={() => onValueChange(item.value)}
		>
			{#if IconComponent}
				<IconComponent class="size-4" />
			{/if}
			{item.label}
			{#if item.badge !== undefined}
				<span
					class="bg-primary text-primary-foreground ml-1 inline-flex min-w-[18px] items-center justify-center rounded-full px-1 text-[10px] font-medium"
				>
					{item.badge}
				</span>
			{/if}
		</Tabs.Trigger>
	{/each}
</Tabs.List>

<style>
	:global(.scrollbar-hide) {
		-ms-overflow-style: none; /* IE and Edge */
		scrollbar-width: none; /* Firefox */
	}

	:global(.scrollbar-hide::-webkit-scrollbar) {
		display: none; /* Chrome, Safari and Opera */
	}
</style>
