<script lang="ts">
	import * as Kbd from '#lib/components/ui/kbd/index.js';
	import { formatShortcutKeys, type ShortcutKey } from '#lib/utils/navigation.js';
	import userStore from '#lib/stores/user-store.svelte.js';

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
			<!-- Shortcut sequences may repeat keys; these keycaps have no local state. -->
			{#each displayKeys as key, index}
				<Kbd.Root>
					{key}
				</Kbd.Root>
				{#if index < displayKeys.length - 1}
					<span class="text-3xs text-muted-foreground/70">+</span>
				{/if}
			{/each}
		</Kbd.Group>
	{/if}
</div>
