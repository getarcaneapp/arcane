<script lang="ts">
	import { Textarea } from '@arcane/ui/textarea';
	import { cn } from '@arcane/ui/utils';
	import type { ComponentProps } from 'svelte';

	let { value = $bindable(), class: className, ...props }: ComponentProps<typeof Textarea> = $props();
</script>

<Textarea
	data-slot="input-group-control"
	class={cn(
		'flex-1 resize-none rounded-none border-0 bg-transparent py-3 shadow-none focus-visible:ring-0 dark:bg-transparent',
		className
	)}
	bind:value
	{...props}
/>
