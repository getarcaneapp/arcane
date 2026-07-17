<script lang="ts">
	import type { WithElementRef } from '$lib/utils.js';
	import type { HTMLOlAttributes } from 'svelte/elements';
	import { cn } from '$lib/utils.js';

	let { ref = $bindable(null), class: className, children, ...restProps }: WithElementRef<HTMLOlAttributes> = $props();
</script>

<ol
	bind:this={ref}
	class={cn('flex flex-wrap items-center gap-1.5 text-sm break-words text-muted-foreground sm:gap-2.5', className)}
	{...restProps}
>
	{@render children?.()}
</ol>
