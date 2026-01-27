<script lang="ts">
	import { cn } from '$lib/utils.js';
	import { LoadingSpinnerIcon } from '$lib/icons';
	import type { FileBrowserLoadingProps } from './types.js';
	import { m } from '$lib/paraglide/messages.js';

	let { class: className, message, ...restProps }: FileBrowserLoadingProps = $props();
</script>

<div
	data-slot="file-browser-loading"
	class={cn('text-muted-foreground flex flex-1 flex-col items-center justify-center gap-2 p-8', className)}
	{...restProps}
>
	<LoadingSpinnerIcon class="size-8" />
	<p class="text-sm">{message || m.common_loading()}</p>
</div>
