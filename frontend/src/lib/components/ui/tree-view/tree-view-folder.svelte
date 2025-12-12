<script lang="ts">
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import { FolderOpenIcon } from '$lib/icons';
	import { cn } from '$lib/utils';
	import type { TreeViewFolderProps } from './types';

	let { name, open = $bindable(true), class: className, icon, children }: TreeViewFolderProps = $props();
</script>

<Collapsible.Root bind:open>
	<Collapsible.Trigger class={cn('flex place-items-center gap-1', className)}>
		{#if icon}
			{@render icon({ name, open })}
		{:else}
			<FolderOpenIcon class="size-4" />
		{/if}
		<span>{name}</span>
	</Collapsible.Trigger>
	<Collapsible.Content class="mx-2 border-l">
		<div class="relative flex place-items-start">
			<div class="bg-border mx-2 h-full w-px"></div>
			<div class="flex flex-col">
				{@render children?.()}
			</div>
		</div>
	</Collapsible.Content>
</Collapsible.Root>
