<script lang="ts">
	import { cn } from '$lib/utils.js';
	import type { FileBrowserRootProps } from './types.js';

	let { class: className, path = '/', loading = false, error = null, children, ...restProps }: FileBrowserRootProps = $props();
</script>

<div
	data-slot="file-browser"
	data-loading={loading ? '' : undefined}
	data-error={error ? '' : undefined}
	class={cn('flex h-full flex-col overflow-hidden rounded-md border', className)}
	{...restProps}
>
	{@render children?.()}
</div>
