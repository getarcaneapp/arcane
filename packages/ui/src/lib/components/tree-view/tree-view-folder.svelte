<script lang="ts">
	import ArrowDownIcon from 'virtual:icons/solar/alt-arrow-down-bold';
	import ArrowRightIcon from 'virtual:icons/solar/alt-arrow-right-bold';
	import FolderOpenIcon from 'virtual:icons/solar/folder-open-bold';

	import * as Collapsible from '#lib/components/collapsible/index.js';
	import { cn } from '#lib/utils.js';

	import type { TreeViewFolderProps } from './types';

	let { name, open = $bindable(false), class: className, icon, children }: TreeViewFolderProps = $props();
</script>

<Collapsible.Root bind:open>
	<Collapsible.Trigger class={cn('flex place-items-center gap-1', className)}>
		{#if open}
			<ArrowDownIcon class="size-4" />
		{:else}
			<ArrowRightIcon class="size-4" />
		{/if}
		{#if icon}
			{@render icon({ name, open })}
		{:else}
			<FolderOpenIcon class="size-4" />
		{/if}
		<span>{name}</span>
	</Collapsible.Trigger>
	<Collapsible.Content class="mx-2 border-l">
		<div class="relative flex place-items-start">
			<div class="mx-2 h-full w-px bg-border"></div>
			<div class="flex flex-col">
				{@render children?.()}
			</div>
		</div>
	</Collapsible.Content>
</Collapsible.Root>
