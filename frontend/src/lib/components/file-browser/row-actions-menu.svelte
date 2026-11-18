<script lang="ts">
	import * as DropdownMenu from '@arcane/ui/dropdown-menu';
	import type { Snippet } from 'svelte';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { EllipsisIcon } from '#lib/icons/index.js';

	let {
		openMenuLabel,
		children
	}: {
		openMenuLabel: string;
		children: Snippet;
	} = $props();
</script>

<DropdownMenu.Root>
	<DropdownMenu.Trigger>
		{#snippet child({ props })}
			<ArcaneButton
				{...props}
				action="base"
				tone="ghost"
				size="icon"
				class="relative size-8 p-0"
				icon={EllipsisIcon}
				showLabel={false}
				customLabel={openMenuLabel}
			/>
		{/snippet}
	</DropdownMenu.Trigger>
	<DropdownMenu.Content align="end">
		<DropdownMenu.Group>
			{@render children()}
		</DropdownMenu.Group>
	</DropdownMenu.Content>
</DropdownMenu.Root>
