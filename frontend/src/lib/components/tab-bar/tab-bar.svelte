<script lang="ts">
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { cn } from '#lib/utils.js';
	import type { TabItem } from './types.ts';

	interface Props {
		items: TabItem[];
		value: string;
		onValueChange: (value: string) => void;
		class?: string;
	}

	let { items, onValueChange, class: className }: Props = $props();
</script>

<Tabs.List scrollable class={cn('inline-flex max-w-full justify-start', className)}>
	{#each items as item (item.value)}
		{@const IconComponent = item.icon}
		<Tabs.Trigger
			value={item.value}
			class="flex-shrink-0 whitespace-nowrap"
			disabled={item.disabled}
			onclick={() => onValueChange(item.value)}
		>
			{#if IconComponent}
				<IconComponent class="size-4" />
			{/if}
			{item.label}
			{#if item.badge !== undefined}
				<span
					class="ml-1 inline-flex min-w-4.5 items-center justify-center rounded-full bg-primary/20 px-1 text-2xs font-semibold text-primary dark:bg-primary/25 dark:text-primary-tint"
				>
					{item.badge}
				</span>
			{/if}
		</Tabs.Trigger>
	{/each}
</Tabs.List>
