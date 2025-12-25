<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { cn } from '$lib/utils.js';
	import { useSidebar } from '$lib/components/ui/sidebar/context.svelte.js';
	import { PinOnIcon, PinOffIcon } from '$lib/icons';

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: {
		ref?: HTMLElement | null;
		class?: string;
		[key: string]: any;
	} = $props();

	const sidebar = useSidebar();
	const isPinned = $derived(sidebar.isPinned);
</script>

<ArcaneButton
	bind:ref
	data-sidebar="pin-button"
	data-slot="sidebar-pin-button"
	action="base"
	tone="ghost"
	size="icon"
	class={cn('text-muted-foreground hover:text-foreground size-7', className)}
	type="button"
	disabled={sidebar.isTablet}
	title={isPinned ? 'Unpin sidebar' : 'Pin sidebar'}
	onclick={(e) => {
		e.preventDefault();
		e.stopPropagation();
		if (!sidebar.isTablet) {
			// Always toggle the pinning preference
			sidebar.toggle();
			// Clear hover state when explicitly pinning/unpinning
			if (sidebar.isHovered) {
				sidebar.setHovered(false);
			}
		}
	}}
	{...restProps}
>
	{#if isPinned}
		<PinOnIcon />
		<span class="sr-only">Unpin sidebar</span>
	{:else}
		<PinOffIcon />
		<span class="sr-only">Pin sidebar</span>
	{/if}
</ArcaneButton>
