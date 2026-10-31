<script lang="ts">
	import { ContextMenu as ContextMenuPrimitive } from 'bits-ui';

	import { cn } from '#lib/utils.js';

	let {
		ref = $bindable(null),
		class: className,
		inset,
		...restProps
	}: ContextMenuPrimitive.GroupHeadingProps & {
		inset?: boolean;
	} = $props();
</script>

<ContextMenuPrimitive.GroupHeading
	bind:ref
	data-slot="context-menu-group-heading"
	data-inset={inset}
	class={cn('px-1.5 py-1 text-xs font-medium text-muted-foreground data-inset:pl-7', className)}
	{...restProps}
/>
