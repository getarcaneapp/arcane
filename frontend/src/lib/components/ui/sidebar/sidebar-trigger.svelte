<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import { cn } from '$lib/utils.js';
	import { ArrowLeftIcon } from '$lib/icons';
	import { useSidebar } from './context.svelte.js';

	let {
		ref = $bindable(null),
		class: className,
		onclick,
		disabled,
		...restProps
	}: {
		ref?: HTMLElement | null;
		class?: string;
		onclick?: (e: MouseEvent) => void;
		disabled?: boolean;
		[key: string]: any;
	} = $props();

	const sidebar = useSidebar();
</script>

<Button
	bind:ref
	data-sidebar="trigger"
	data-slot="sidebar-trigger"
	variant="ghost"
	size="icon"
	class={cn('size-7', className)}
	type="button"
	disabled={disabled || sidebar.isTablet}
	onclick={(e) => {
		onclick?.(e);
		if (!sidebar.isTablet) {
			sidebar.toggle();
		}
	}}
	{...restProps}
>
	<ArrowLeftIcon />
	<span class="sr-only">Toggle Sidebar</span>
</Button>
