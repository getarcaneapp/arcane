<script lang="ts">
	import type { ComponentProps } from 'svelte';

	import { Separator } from '#lib/components/separator/index.js';
	import { cn } from '#lib/utils.js';

	let { ref = $bindable(null), class: className, ...restProps }: ComponentProps<typeof Separator> = $props();
</script>

<Separator bind:ref data-slot="item-separator" orientation="horizontal" class={cn('my-0', className)} {...restProps} />
