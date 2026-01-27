<script lang="ts">
	import { cn } from '$lib/utils.js';
	import { FolderXIcon } from '$lib/icons';
	import type { FileBrowserEmptyProps } from './types.js';
	import { m } from '$lib/paraglide/messages.js';

	let { class: className, message, ...restProps }: FileBrowserEmptyProps = $props();
</script>

<div
	data-slot="file-browser-empty"
	class={cn('text-muted-foreground flex flex-1 flex-col items-center justify-center gap-2 p-8', className)}
	{...restProps}
>
	<FolderXIcon class="size-12 opacity-50" />
	<p class="text-sm">{message || m.file_browser_empty_directory()}</p>
</div>
