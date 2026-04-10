<script lang="ts">
	import type { Snippet } from 'svelte';
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import * as Sidebar from '$lib/components/ui/sidebar/index.js';
	import { ArrowRightIcon } from '$lib/icons';
	import SidebarItemTooltipContent from './sidebar-item-tooltip-content.svelte';
	import type { ShortcutKey } from '$lib/utils/keyboard-shortcut.utils';

	let {
		item,
		showTooltip,
		includeTitleInTooltip,
		getIsOpen,
		onOpenChange,
		content
	}: {
		item: {
			title: string;
			url: string;
			icon?: typeof ArrowRightIcon;
			shortcut?: ShortcutKey[];
			isActive: boolean;
		};
		showTooltip: boolean;
		includeTitleInTooltip: boolean;
		getIsOpen: (title: string, isActive: boolean) => boolean;
		onOpenChange: (open: boolean) => void;
		content?: Snippet;
	} = $props();
</script>

{#snippet tooltipContent()}
	<SidebarItemTooltipContent title={item.title} shortcut={item.shortcut} includeTitle={includeTitleInTooltip} />
{/snippet}

<Collapsible.Root open={getIsOpen(item.title, item.isActive)} {onOpenChange} class="group/collapsible">
	<Sidebar.MenuItem class="flex-col">
		{@const Icon = item.icon}
		<Sidebar.MenuButton tooltipContent={showTooltip ? tooltipContent : undefined} isActive={item.isActive}>
			{#snippet child({ props })}
				<a href={item.url} {...props}>
					{#if item.icon}
						<Icon />
					{/if}
					<span>{item.title}</span>
				</a>
			{/snippet}
		</Sidebar.MenuButton>
		<Collapsible.Trigger>
			{#snippet child({ props })}
				<Sidebar.MenuAction {...props} aria-label="Toggle submenu" class="data-[state=open]:bg-sidebar-accent">
					<ArrowRightIcon class="transition-transform duration-200 group-data-[state=open]/collapsible:rotate-90" />
				</Sidebar.MenuAction>
			{/snippet}
		</Collapsible.Trigger>
		{#if content}
			{@render content()}
		{/if}
	</Sidebar.MenuItem>
</Collapsible.Root>
