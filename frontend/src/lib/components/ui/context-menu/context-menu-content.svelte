<script lang="ts">
	import { cn } from '$lib/utils.js';
	import { ContextMenu as ContextMenuPrimitive } from 'bits-ui';

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: ContextMenuPrimitive.ContentProps = $props();
</script>

<ContextMenuPrimitive.Portal>
	<ContextMenuPrimitive.Content
		bind:ref
		data-slot="context-menu-content"
		class={cn(
			'bg-background/95 text-popover-foreground data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 dark:bg-popover/20 z-50 min-w-32 overflow-hidden rounded-xl border p-1 shadow-md backdrop-blur-2xl backdrop-saturate-150 outline-none',
			className
		)}
		{...restProps}
	/>
</ContextMenuPrimitive.Portal>
