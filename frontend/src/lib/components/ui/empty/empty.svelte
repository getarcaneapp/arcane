<script lang="ts">
	import type { HTMLAttributes } from 'svelte/elements';

	import { cn } from '#lib/utils.js';

	let {
		class: className,
		variant = 'plain',
		children,
		...restProps
	}: HTMLAttributes<HTMLDivElement> & {
		variant?: 'plain' | 'dropzone';
	} = $props();
</script>

<div
	data-slot="empty"
	class={cn(
		'flex min-w-0 flex-1 flex-col items-center justify-center gap-6 rounded-lg p-6 py-12 text-center text-balance md:p-12',
		variant === 'dropzone' && 'border border-dashed border-border/50 bg-muted/20',
		className
	)}
	{...restProps}
>
	{@render children?.()}
</div>
