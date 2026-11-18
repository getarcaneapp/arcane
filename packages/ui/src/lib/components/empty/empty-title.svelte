<script lang="ts">
	import type { HTMLAttributes } from 'svelte/elements';

	import { cn } from '#lib/utils.js';

	let { class: className, children, ...restProps }: HTMLAttributes<HTMLDivElement> = $props();
</script>

<div data-slot="empty-title" class={cn('text-lg font-medium tracking-tight', className)} {...restProps}>
	{@render children?.()}
</div>
