<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
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

<ArcaneButton
	bind:ref
	data-sidebar="trigger"
	data-slot="sidebar-trigger"
	action="base"
	tone="ghost"
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
</ArcaneButton>
