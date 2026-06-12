<script lang="ts">
	import type { Snippet } from 'svelte';
	import { cn } from '$lib/utils';

	interface Props {
		children: Snippet;
		class?: string;
	}

	let { children, class: className }: Props = $props();
</script>

<div
	class={cn(
		'bg-card/60 backdrop-blur-md border-border/70 relative overflow-hidden rounded-xl border p-4 shadow-xs sm:p-5',
		className
	)}
>
	<div class="relative">
		{@render children()}
	</div>
</div>
