<script lang="ts">
	import * as Kbd from '$lib/components/ui/kbd/index.js';
	import { formatShortcutKeys, type ShortcutKey } from '$lib/utils/keyboard-shortcut.utils';

	let {
		title,
		shortcut,
		includeTitle = true
	}: {
		title: string;
		shortcut?: ShortcutKey[];
		includeTitle?: boolean;
	} = $props();
</script>

<div class="flex flex-wrap items-center gap-2">
	{#if includeTitle}
		<span>{title}</span>
	{/if}
	{#if shortcut?.length}
		{@const displayKeys = formatShortcutKeys(shortcut)}
		<Kbd.Group class="text-muted-foreground inline-flex items-center gap-1">
			{#each displayKeys as key, index}
				<Kbd.Root
					class="text-popover-foreground! in-data-[slot=tooltip-content]:text-popover-foreground! dark:in-data-[slot=tooltip-content]:text-popover-foreground!"
				>
					{key}
				</Kbd.Root>
				{#if index < displayKeys.length - 1}
					<span class="text-muted-foreground/70 text-[10px]">+</span>
				{/if}
			{/each}
		</Kbd.Group>
	{/if}
</div>
