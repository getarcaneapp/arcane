<script lang="ts">
	import { cn } from '$lib/utils.js';
	import { AlertIcon } from '$lib/icons';
	import { Button } from '$lib/components/ui/button/index.js';
	import type { FileBrowserErrorProps } from './types.js';
	import { m } from '$lib/paraglide/messages.js';

	let { class: className, message, onRetry, ...restProps }: FileBrowserErrorProps = $props();
</script>

<div
	data-slot="file-browser-error"
	class={cn('flex flex-1 flex-col items-center justify-center gap-3 p-8', className)}
	{...restProps}
>
	<AlertIcon class="text-destructive size-12 opacity-70" />
	<p class="text-muted-foreground max-w-sm text-center text-sm">{message}</p>
	{#if onRetry}
		<Button variant="outline" size="sm" onclick={onRetry}>
			{m.common_retry()}
		</Button>
	{/if}
</div>
