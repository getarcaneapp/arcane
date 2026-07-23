<script lang="ts">
	import type { Snippet } from 'svelte';
	import { cn } from '#lib/utils';

	interface Props {
		children: Snippet;
		class?: string;
	}

	let { children, class: className }: Props = $props();
</script>

<div
	class={cn(
		'relative overflow-hidden rounded-xl border border-border/70 bg-card/60 p-4 shadow-xs backdrop-blur-md sm:p-5',
		className
	)}
>
	<div class="relative">
		{@render children()}
	</div>
</div>
