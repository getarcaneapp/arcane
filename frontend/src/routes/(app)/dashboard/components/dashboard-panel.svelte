<script lang="ts">
	import type { Snippet } from 'svelte';

	import * as Card from '#lib/components/ui/card/index.js';

	let {
		title,
		actions,
		children
	}: {
		title: string;
		actions?: Snippet;
		children: Snippet;
	} = $props();
</script>

<Card.Root variant="transparent" role="region" aria-label={title} interactive class="h-full min-w-0">
	<div class="flex h-full flex-col gap-3 p-5">
		<div class="flex min-h-7 items-center justify-between gap-2">
			<h2 class="truncate text-sm font-medium">{title}</h2>
			{@render actions?.()}
		</div>
		<div class="-mx-2 min-h-0 flex-1 overflow-y-auto px-2">
			{@render children()}
		</div>
	</div>
</Card.Root>
