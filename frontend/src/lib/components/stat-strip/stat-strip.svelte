<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { ClassValue } from 'svelte/elements';
	import { cn } from '#lib/utils.js';

	interface Props {
		/** Column classes, e.g. `lg:grid-cols-4`. */
		class?: ClassValue;
		children: Snippet;
	}

	let { class: className, children }: Props = $props();
</script>

<dl class={cn('grid grid-cols-2 gap-y-6 border-y border-border/50 py-6 lg:divide-x lg:divide-border/50', className)}>
	{@render children()}
</dl>
