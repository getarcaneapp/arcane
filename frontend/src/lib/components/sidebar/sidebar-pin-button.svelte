<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import { cn } from '$lib/utils.js';
	import PinIcon from 'phosphor-svelte/lib/PushPin';
	import PinOffIcon from 'phosphor-svelte/lib/PushPinSlash';
	import { useSidebar } from '$lib/components/ui/sidebar/context.svelte.js';

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

<Button
	bind:ref
	data-sidebar="pin-button"
	data-slot="sidebar-pin-button"
	variant="ghost"
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
		<PinOffIcon size={16} />
		<span class="sr-only">Unpin sidebar</span>
	{:else}
		<PinIcon size={16} />
		<span class="sr-only">Pin sidebar</span>
	{/if}
</Button>
