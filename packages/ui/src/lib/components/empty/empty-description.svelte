<script lang="ts">
	import type { HTMLAttributes } from 'svelte/elements';

	import { cn } from '#lib/utils.js';

	let { class: className, children, ...restProps }: HTMLAttributes<HTMLDivElement> = $props();
</script>

<div
	data-slot="empty-description"
	class={cn(
		'text-sm/relaxed wrap-break-word text-muted-foreground [&>a]:underline [&>a]:underline-offset-4 [&>a:hover]:text-primary-text',
		className
	)}
	{...restProps}
>
	{@render children?.()}
</div>
