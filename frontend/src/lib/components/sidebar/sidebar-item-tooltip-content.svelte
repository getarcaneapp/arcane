<script lang="ts">
	import * as Kbd from '#lib/components/ui/kbd/index.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import { withOccurrenceKeys } from '#lib/utils/formatting.js';
	import { formatShortcutKeys, type ShortcutKey } from '#lib/utils/navigation.js';

	let {
		title,
		shortcut,
		includeTitle = true
	}: {
		title: string;
		shortcut?: ShortcutKey[];
		includeTitle?: boolean;
	} = $props();

	const showShortcut = $derived(userStore.current?.preferences?.keyboardShortcutsEnabled ?? true);
</script>

<div class="flex flex-wrap items-center gap-2">
	{#if includeTitle}
		<span>{title}</span>
	{/if}
	{#if showShortcut && shortcut?.length}
		{@const displayKeys = formatShortcutKeys(shortcut)}
		<Kbd.Group>
			{#each withOccurrenceKeys(displayKeys) as key, index (key.key)}
				<Kbd.Root>
					{key.value}
				</Kbd.Root>
				{#if index < displayKeys.length - 1}
					<span class="text-3xs text-muted-foreground/70">+</span>
				{/if}
			{/each}
		</Kbd.Group>
	{/if}
</div>
