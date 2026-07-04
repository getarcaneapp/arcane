<script lang="ts">
	import { cn } from '$lib/utils';
	import type { Snippet } from 'svelte';

	interface Props {
		class?: string;
		children: Snippet;
	}

	let { class: className, children }: Props = $props();
</script>

<!-- One glass panel per page/tab; sections inside are separated by dividers. -->
<div class={cn('bg-card/60 dark:bg-surface/40 border-border/70 rounded-xl border shadow-xs backdrop-blur-md', className)}>
	<div class="divide-border/50 divide-y">
		{@render children()}
	</div>
</div>
