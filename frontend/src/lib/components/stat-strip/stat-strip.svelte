<script lang="ts">
	import { cn } from '@arcane/ui/utils';
	import type { Snippet } from 'svelte';
	import type { ClassValue } from 'svelte/elements';

	interface Props {
		class?: ClassValue;
		children: Snippet;
	}

	let { class: className, children }: Props = $props();
</script>

<dl class={cn('grid grid-cols-2 gap-y-6 border-y border-border/50 py-6 lg:divide-x lg:divide-border/50', className)}>
	{@render children()}
</dl>
