<script lang="ts">
	import { cn } from '$lib/utils.js';
	import { FolderOpenIcon, ArrowRightIcon } from '$lib/icons';
	import type { FileBrowserBreadcrumbProps } from './types.js';

	let { class: className, path, onNavigate, ...restProps }: FileBrowserBreadcrumbProps = $props();

	const segments = $derived(() => {
		const parts = path.split('/').filter(Boolean);
		const result: { name: string; path: string }[] = [{ name: '/', path: '/' }];

		let currentPath = '';
		for (const part of parts) {
			currentPath += '/' + part;
			result.push({ name: part, path: currentPath });
		}

		return result;
	});

	function handleClick(segmentPath: string) {
		onNavigate?.(segmentPath);
	}
</script>

<div
	data-slot="file-browser-breadcrumb"
	class={cn('bg-muted/50 flex items-center gap-1 border-b px-3 py-2', className)}
	{...restProps}
>
	{#each segments() as segment, i}
		{#if i > 0}
			<ArrowRightIcon class="text-muted-foreground size-3" />
		{/if}
		<button
			type="button"
			class="hover:bg-accent flex items-center gap-1 rounded px-1.5 py-0.5 text-sm transition-colors"
			class:font-medium={i === segments().length - 1}
			onclick={() => handleClick(segment.path)}
		>
			{#if i === 0}
				<FolderOpenIcon class="size-4" />
			{/if}
			<span class="max-w-[150px] truncate">{segment.name}</span>
		</button>
	{/each}
</div>
