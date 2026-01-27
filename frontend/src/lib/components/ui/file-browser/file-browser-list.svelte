<script lang="ts" generics="T extends FileBrowserFile">
	import { cn } from '$lib/utils.js';
	import type { FileBrowserListProps, FileBrowserFile } from './types.js';
	import FileBrowserItem from './file-browser-item.svelte';

	let { class: className, files, selectedPath = null, onSelect, onOpen, icon, ...restProps }: FileBrowserListProps<T> = $props();

	// Sort files: directories first, then by name
	const sortedFiles = $derived(() => {
		return [...files].sort((a, b) => {
			// Directories first
			if (a.type === 'directory' && b.type !== 'directory') return -1;
			if (a.type !== 'directory' && b.type === 'directory') return 1;
			// Then alphabetically
			return a.name.localeCompare(b.name);
		});
	});

	function handleItemClick(file: T) {
		// Directories navigate on single click
		if (file.type === 'directory') {
			onOpen?.(file);
		} else {
			// Files get selected on single click
			onSelect?.(file);
		}
	}
</script>

<div data-slot="file-browser-list" class={cn('flex-1 overflow-auto', className)} {...restProps}>
	<div class="divide-y">
		{#each sortedFiles() as file (file.path)}
			<FileBrowserItem {file} selected={selectedPath === file.path} {icon} onclick={() => handleItemClick(file)} />
		{/each}
	</div>
</div>
