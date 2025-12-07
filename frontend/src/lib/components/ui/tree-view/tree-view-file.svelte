<script lang="ts">
	import FileIcon from 'phosphor-svelte/lib/File';
	import { cn } from '$lib/utils';
	import type { TreeViewFileProps } from './types';

	let { name, icon, type = 'button', class: className, ...rest }: TreeViewFileProps = $props();
</script>

<button {type} class={cn('flex place-items-center gap-1 pl-[3px]', className)} {...rest}>
	{#if icon}
		{@render icon({ name })}
	{:else}
		<FileIcon class="size-4" />
	{/if}
	<span>{name}</span>
</button>
