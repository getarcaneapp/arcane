<script lang="ts">
	import { Separator } from '@arcane/ui/separator';
	import { cn } from '@arcane/ui/utils';
	import type { ComponentProps } from 'svelte';

	let { ref = $bindable(null), class: className, ...restProps }: ComponentProps<typeof Separator> = $props();
</script>

<Separator
	bind:ref
	data-slot="sidebar-separator"
	data-sidebar="separator"
	class={cn('mx-2 w-auto bg-sidebar-border', className)}
	{...restProps}
/>
