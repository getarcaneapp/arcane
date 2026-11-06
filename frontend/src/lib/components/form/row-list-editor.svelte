<script lang="ts" generics="T">
	import type { Snippet } from 'svelte';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { AddIcon, CloseIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	let {
		rows = $bindable([]),
		createRow,
		children,
		disabled = false,
		addLabel = m.common_add()
	}: {
		rows?: T[];
		createRow: () => T;
		children: Snippet<[T]>;
		disabled?: boolean;
		addLabel?: string;
	} = $props();
</script>

<div class="space-y-3">
	{#each rows as row, index (row)}
		<div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:gap-3">
			{@render children(row)}
			<ArcaneButton
				action="base"
				tone="ghost"
				size="icon"
				onclick={() => rows.splice(index, 1)}
				{disabled}
				class="shrink-0 text-destructive hover:text-destructive"
				icon={CloseIcon}
			/>
		</div>
	{/each}
	<ArcaneButton
		action="base"
		tone="outline"
		onclick={() => rows.push(createRow())}
		{disabled}
		class="w-fit"
		icon={AddIcon}
		customLabel={addLabel}
	/>
</div>
