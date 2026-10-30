<script lang="ts">
	import type { Snippet } from 'svelte';

	import { cn } from '#lib/utils.js';

	interface Props {
		children: Snippet;
		class?: string;
	}

	let { children, class: className }: Props = $props();
</script>

<div class={cn('relative overflow-hidden rounded-xl border border-border/70 bg-card/10 p-4 sm:p-5', className)}>
	<div class="relative">
		{@render children()}
	</div>
</div>
