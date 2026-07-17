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
			'flex h-10 w-full rounded-lg border bg-input/80 px-3 py-2 text-base ring-offset-background backdrop-blur-sm transition-all file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground focus-visible:bg-input/90 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:backdrop-blur-md focus-visible:outline-none disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 md:text-sm',
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
			'flex h-9 w-full min-w-0 rounded-lg bg-input/80 px-3 py-1 text-base ring-offset-background backdrop-blur-sm transition-all outline-none selection:bg-primary selection:text-primary-foreground placeholder:text-muted-foreground disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 md:text-sm',
			'focus-visible:border-ring focus-visible:bg-input/90 focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:backdrop-blur-md',
			'aria-invalid:border-destructive aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40',
			className
		)}
		{type}
		bind:value
		{...restProps}
	/>
{/if}
