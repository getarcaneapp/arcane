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
	{#snippet child({ props })}
		<Sidebar.MenuItem {...props}>
			<Collapsible.Trigger>
				{#snippet child({ props })}
					{@const Icon = item.icon}
					<Sidebar.MenuButton tooltipContent={showTooltip ? tooltipContent : undefined} isActive={item.isActive}>
						{#snippet child({ props })}
							<a href={item.url} {...props}>
								{#if item.icon}
									<Icon />
								{/if}
								<span>{item.title}</span>
								<ArrowRightIcon class="ml-auto transition-transform duration-200 group-data-[state=open]/collapsible:rotate-90" />
							</a>
						{/snippet}
					</Sidebar.MenuButton>
				{/snippet}
			</Collapsible.Trigger>
			{#if content}
				{@render content()}
			{/if}
		</Sidebar.MenuItem>
	{/snippet}
</Collapsible.Root>
