<script lang="ts">
	import type { Snippet } from 'svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { EllipsisIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';

	let {
		children,
		triggerClass = 'size-8',
		iconClass = 'size-4'
	}: {
		children: Snippet;
		triggerClass?: string;
		iconClass?: string;
	} = $props();
</script>

<DropdownMenu.Root>
	<DropdownMenu.Trigger>
		{#snippet child({ props })}
			<ArcaneButton {...props} action="base" tone="ghost" size="icon" class={triggerClass}>
				<span class="sr-only">{m.common_open_menu()}</span>
				<EllipsisIcon class={iconClass} />
			</ArcaneButton>
		{/snippet}
	</DropdownMenu.Trigger>
	<DropdownMenu.Content align="end">
		<DropdownMenu.Group>
			{@render children()}
		</DropdownMenu.Group>
	</DropdownMenu.Content>
</DropdownMenu.Root>
