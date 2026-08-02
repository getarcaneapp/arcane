<script lang="ts">
	import { cn } from '#lib/utils.js';
	import { DropdownMenu as DropdownMenuPrimitive } from 'bits-ui';
	import { getContext } from 'svelte';
	import { dropdownMenuContextKey, type DropdownMenuContext } from './dropdown-menu-context';

	let {
		ref = $bindable(null),
		class: className,
		inset,
		variant = 'default',
		closeOnSelect = true,
		onclick,
		...restProps
	}: DropdownMenuPrimitive.ItemProps & {
		inset?: boolean;
		variant?: 'default' | 'destructive';
		closeOnSelect?: boolean;
		onclick?: (event: MouseEvent) => void;
	} = $props();

	const menuContext = getContext<DropdownMenuContext | null>(dropdownMenuContextKey);

	function handleClick(event: MouseEvent) {
		onclick?.(event);
		if (closeOnSelect) {
			menuContext?.close();
		}
	}
</script>

<DropdownMenuPrimitive.Item
	bind:ref
	data-slot="dropdown-menu-item"
	data-inset={inset}
	data-variant={variant}
	onclick={handleClick}
	class={cn(
		"relative flex cursor-default items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-hidden select-none data-highlighted:bg-accent data-highlighted:text-accent-foreground data-[disabled]:pointer-events-none data-[disabled]:opacity-50 data-[inset]:pl-8 data-[variant=destructive]:text-destructive data-[variant=destructive]:data-highlighted:bg-destructive/10 data-[variant=destructive]:data-highlighted:text-destructive dark:data-[variant=destructive]:data-highlighted:bg-destructive/20 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4 [&_svg:not([class*='text-'])]:text-muted-foreground data-[variant=destructive]:*:[svg]:!text-destructive",
		className
	)}
	{...restProps}
/>
