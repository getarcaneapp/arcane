<script lang="ts">
	import { cn } from '$lib/utils.js';
	import { FolderOpenIcon, FileTextIcon } from '$lib/icons';
	import type { FileBrowserItemProps } from './types.js';
	import bytes from 'bytes';

	let { class: className, file, selected = false, icon, ...restProps }: FileBrowserItemProps = $props();

	const formattedSize = $derived(() => {
		if (file.type === 'directory') return '';
		return file.size != null ? bytes(file.size) || '0 B' : '';
	});

	const displayMode = $derived(() => {
		if (!file.mode) return '';
		return file.mode;
	});
</script>

<button
	type="button"
	data-slot="file-browser-item"
	data-type={file.type}
	data-selected={selected ? '' : undefined}
	class={cn(
		'hover:bg-accent/50 flex w-full items-center gap-3 px-3 py-2 text-left text-sm transition-colors',
		selected && 'bg-accent',
		className
	)}
	{...restProps}
>
	<div class="shrink-0">
		{#if icon}
			{@render icon({ file })}
		{:else if file.type === 'directory'}
			<FolderOpenIcon class="text-primary size-4" />
		{:else}
			<FileTextIcon class="text-muted-foreground size-4" />
		{/if}
	</div>

	<div class="min-w-0 flex-1">
		<div class="flex items-center gap-2">
			<span class="truncate font-medium">{file.name}</span>
			{#if file.type === 'symlink' && file.linkTarget}
				<span class="text-muted-foreground truncate text-xs">→ {file.linkTarget}</span>
			{/if}
		</div>
	</div>

	<div class="text-muted-foreground flex shrink-0 items-center gap-4 text-xs">
		{#if displayMode()}
			<span class="hidden font-mono sm:inline">{displayMode()}</span>
		{/if}
		{#if formattedSize()}
			<span class="w-16 text-right">{formattedSize()}</span>
		{/if}
	</div>
</button>
