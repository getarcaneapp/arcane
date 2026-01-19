<script lang="ts" generics="T extends FileBrowserFile">
	import { cn } from '$lib/utils.js';
	import { FileTextIcon, LoadingSpinnerIcon, AlertIcon } from '$lib/icons';
	import type { FileBrowserPreviewProps, FileBrowserFile } from './types.js';
	import { m } from '$lib/paraglide/messages.js';
	import bytes from 'bytes';

	let {
		class: className,
		file,
		content,
		loading = false,
		isBinary = false,
		truncated = false,
		error = null,
		...restProps
	}: FileBrowserPreviewProps<T> = $props();
</script>

<div data-slot="file-browser-preview" class={cn('flex flex-col overflow-hidden border-l', className)} {...restProps}>
	{#if !file}
		<div class="text-muted-foreground flex flex-1 items-center justify-center p-4 text-sm">
			{m.file_browser_select_file()}
		</div>
	{:else}
		<div class="bg-muted/50 flex items-center gap-2 border-b px-3 py-2">
			<FileTextIcon class="size-4" />
			<span class="flex-1 truncate text-sm font-medium">{file.name}</span>
			{#if file.size != null}
				<span class="text-muted-foreground text-xs">{bytes(file.size)}</span>
			{/if}
		</div>

		<div class="flex-1 overflow-auto">
			{#if loading}
				<div class="flex h-full items-center justify-center">
					<LoadingSpinnerIcon class="size-6" />
				</div>
			{:else if error}
				<div class="flex h-full flex-col items-center justify-center gap-2 p-4">
					<AlertIcon class="text-destructive size-8" />
					<p class="text-muted-foreground text-sm">{error}</p>
				</div>
			{:else if isBinary}
				<div class="text-muted-foreground flex h-full items-center justify-center p-4 text-sm">
					{m.file_browser_binary_file()}
				</div>
			{:else if content != null}
				<pre class="bg-muted/30 h-full overflow-auto p-3 font-mono text-xs leading-relaxed">{content}</pre>
				{#if truncated}
					<div class="bg-warning/10 text-warning border-t px-3 py-1.5 text-xs">
						{m.file_browser_content_truncated()}
					</div>
				{/if}
			{/if}
		</div>
	{/if}
</div>
