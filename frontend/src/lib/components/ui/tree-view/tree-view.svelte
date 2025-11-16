<script lang="ts">
	import { cn } from '$lib/utils';
	import type { TreeViewRootProps } from './types';

	let { children, class: className }: TreeViewRootProps = $props();
</script>

<div class={cn('flex flex-col', className)}>
	{@render children?.()}
</div>
