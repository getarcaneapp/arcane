<script lang="ts">
	import { Dialog as DialogPrimitive } from 'bits-ui';
	import { CloseIcon } from '$lib/icons';
	import type { Snippet } from 'svelte';
	import Overlay from './dialog-overlay.svelte';
	import { cn, type WithoutChildrenOrChild } from '$lib/utils.js';
	import { m } from '$lib/paraglide/messages';

	let {
		ref = $bindable(null),
		class: className,
		portalProps,
		children,
		showCloseButton = true,
		overlayClass,
		...restProps
	}: WithoutChildrenOrChild<DialogPrimitive.ContentProps> & {
		portalProps?: DialogPrimitive.PortalProps;
		children: Snippet;
		showCloseButton?: boolean;
		overlayClass?: string;
	} = $props();
</script>

<DialogPrimitive.Portal {...portalProps}>
	<Overlay class={overlayClass} />
	<DialogPrimitive.Content
		bind:ref
		data-slot="dialog-content"
		class={cn(
			'fixed top-[50%] left-[50%] z-[var(--arcane-z-surface)] grid w-full max-w-lg translate-x-[-50%] translate-y-[-50%] gap-4 rounded-2xl border border-border/30 bg-white p-6 text-foreground shadow-lg backdrop-blur-md duration-200 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:fill-mode-forwards data-[state=closed]:zoom-out-95 data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95 sm:rounded-2xl dark:border-border/80 dark:bg-surface/10',
			className
		)}
		{...restProps}
	>
		{@render children?.()}
		{#if showCloseButton}
			<DialogPrimitive.Close
				class="absolute top-4 right-4 rounded-xs opacity-70 ring-offset-background transition-opacity hover:opacity-100 focus:ring-2 focus:ring-ring focus:ring-offset-2 focus:outline-hidden disabled:pointer-events-none [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4"
			>
				<CloseIcon />
				<span class="sr-only">{m.common_close()}</span>
			</DialogPrimitive.Close>
		{/if}
	</DialogPrimitive.Content>
</DialogPrimitive.Portal>
