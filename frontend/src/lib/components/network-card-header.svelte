<script lang="ts">
	import { NetworksIcon } from '$lib/icons';
	import type { Snippet } from 'svelte';

	interface Props {
		name: string;
		children?: Snippet;
	}

	let { name, children }: Props = $props();
</script>

<div class="mb-4 flex items-center gap-3 border-b border-border pb-4">
	<div class="rounded-lg bg-blue-500/10 p-2">
		<NetworksIcon class="size-5 text-blue-500" />
	</div>
	<div class="min-w-0 flex-1">
		<div class="text-base font-semibold break-all text-foreground">
			{name}
		</div>
		{#if children}
			{@render children()}
		{/if}
	</div>
</div>
