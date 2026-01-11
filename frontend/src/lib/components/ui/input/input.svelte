<script lang="ts">
	import type { HTMLInputAttributes, HTMLInputTypeAttribute } from 'svelte/elements';
	import { cn, type WithElementRef } from '$lib/utils.js';

	type InputType = Exclude<HTMLInputTypeAttribute, 'file'>;

	type Props = WithElementRef<
		Omit<HTMLInputAttributes, 'type'> & ({ type: 'file'; files?: FileList } | { type?: InputType; files?: undefined })
	>;

	let { ref = $bindable(null), value = $bindable(), type, files = $bindable(), class: className, ...restProps }: Props = $props();
</script>

{#if type === 'file'}
	<input
		bind:this={ref}
		data-slot="input"
		class={cn(
			'bg-input/80 ring-offset-background placeholder:text-muted-foreground focus-visible:ring-ring focus-visible:bg-input/90 flex h-10 w-full rounded-lg border px-3 py-2 text-base backdrop-blur-sm transition-all file:border-0 file:bg-transparent file:text-sm file:font-medium focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:backdrop-blur-md focus-visible:outline-none disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 md:text-sm',
			className
		)}
		type="file"
		bind:files
		bind:value
		{...restProps}
	/>
{:else}
	<input
		bind:this={ref}
		data-slot="input"
		class={cn(
			'bg-input/80 selection:bg-primary selection:text-primary-foreground ring-offset-background placeholder:text-muted-foreground flex h-9 w-full min-w-0 rounded-lg px-3 py-1 text-base backdrop-blur-sm transition-all outline-none disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 md:text-sm',
			'focus-visible:bg-input/90 focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px] focus-visible:backdrop-blur-md',
			'aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive',
			className
		)}
		{type}
		bind:value
		{...restProps}
	/>
{/if}
