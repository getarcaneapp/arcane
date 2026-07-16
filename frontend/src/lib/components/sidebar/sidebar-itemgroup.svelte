<script lang="ts">
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import * as Sidebar from '$lib/components/ui/sidebar/index.js';
	import { page } from '$app/state';
	import { useSidebar } from '$lib/components/ui/sidebar/context.svelte.js';
	import { matchesNavigationPath, type ShortcutKey } from '$lib/utils/navigation';
	import type { Snippet } from 'svelte';
	import { ArrowRightIcon } from '$lib/icons';
	import SidebarCollapsibleItem from './sidebar-collapsible-item.svelte';
	import SidebarItemTooltipContent from './sidebar-item-tooltip-content.svelte';
	import settingsStore from '$lib/stores/config-store';

	let {
		items,
		label
	}: {
		label: string;
		items: {
			title: string;
			url: string;
			activePrefixes?: string[];
			icon?: typeof ArrowRightIcon;
			shortcut?: ShortcutKey[];
			items?: {
				title: string;
				url: string;
				activePrefixes?: string[];
				icon?: typeof ArrowRightIcon;
				shortcut?: ShortcutKey[];
			}[];
		}[];
	} = $props();

	const sidebar = useSidebar();

	function isActiveItem(item: { url: string; activePrefixes?: string[] }): boolean {
		// Special case: Don't highlight "Environments" when on GitOps page
		if (item.url === '/environments' && page.url.pathname.includes('/gitops')) {
			return false;
		}
		return matchesNavigationPath(page.url.pathname, item);
	}

	function hasActiveChild(items?: { url: string; activePrefixes?: string[] }[]): boolean {
		return items?.some((child) => isActiveItem(child)) ?? false;
	}

	let openStates = $state<Record<string, boolean>>({});

	const enhancedItems = $derived(
		items.map((item) => {
			const isItemActive = isActiveItem(item);
			const hasActiveSubItem = hasActiveChild(item.items);
			const isActive = isItemActive || hasActiveSubItem;

			return {
				...item,
				isActive,
				items: item.items?.map((subItem) => ({
					...subItem,
					isActive: isActiveItem(subItem)
				}))
			};
		})
	);

	function getIsOpen(itemUrl: string, isActive: boolean): boolean {
		if (openStates[itemUrl] === undefined) {
			return isActive;
		}
		return openStates[itemUrl];
	}

	const collapsed = $derived(sidebar.state === 'collapsed');
	const hoverCollapsed = $derived(collapsed && sidebar.hoverExpansionEnabled && !sidebar.isHovered);
	const includeTitleInTooltip = $derived(collapsed && !(sidebar.hoverExpansionEnabled && sidebar.isHovered));
	const shortcutsEnabled = $derived($settingsStore?.keyboardShortcutsEnabled ?? true);
</script>

{#snippet itemAnchor(entry: { title: string; url: string; icon?: typeof ArrowRightIcon }, props: Record<string, unknown>)}
	{@const Icon = entry.icon}
	<a href={entry.url} {...props}>
		{#if entry.icon}
			<Icon />
		{/if}
		<span>{entry.title}</span>
	</a>
{/snippet}

{#snippet menuEntry(
	entry: { title: string; url: string; icon?: typeof ArrowRightIcon; isActive: boolean },
	tooltip: Snippet | undefined
)}
	<Sidebar.MenuItem>
		<Sidebar.MenuButton isActive={entry.isActive} tooltipContent={tooltip}>
			{#snippet child({ props })}
				{@render itemAnchor(entry, props)}
			{/snippet}
		</Sidebar.MenuButton>
	</Sidebar.MenuItem>
{/snippet}

<Sidebar.Group class={hoverCollapsed ? 'items-center p-1.5' : 'p-1.5'}>
	{#if !hoverCollapsed}
		<Sidebar.GroupLabel class="h-7 px-1.5">{label}</Sidebar.GroupLabel>
	{/if}
	<Sidebar.Menu class={hoverCollapsed ? 'w-8 gap-0.5' : 'gap-0.5'}>
		{#each enhancedItems as item (item.url)}
			{#if (item.items?.length ?? 0) > 0 && !hoverCollapsed}
				{#snippet collapsibleSubMenu()}
					<Collapsible.Content>
						<Sidebar.MenuSub
							class={sidebar.state === 'collapsed' && (!sidebar.isHovered || !sidebar.hoverExpansionEnabled)
								? 'hidden'
								: undefined}
						>
							{#each item.items ?? [] as subItem (subItem.url)}
								<Sidebar.MenuSubItem>
									<Sidebar.MenuSubButton isActive={subItem.isActive} size="md">
										{#snippet child({ props })}
											{@render itemAnchor(subItem, props)}
										{/snippet}
									</Sidebar.MenuSubButton>
								</Sidebar.MenuSubItem>
							{/each}
						</Sidebar.MenuSub>
					</Collapsible.Content>
				{/snippet}
				<SidebarCollapsibleItem
					{item}
					showTooltip={collapsed || (shortcutsEnabled && !!item.shortcut?.length)}
					{includeTitleInTooltip}
					getIsOpen={(itemUrl: string, isActive: boolean) => getIsOpen(itemUrl, isActive)}
					onOpenChange={(open) => {
						openStates[item.url] = open;
					}}
					content={collapsibleSubMenu}
				/>
			{:else}
				{#snippet simpleItemTooltipContent()}
					<SidebarItemTooltipContent title={item.title} shortcut={item.shortcut} includeTitle={includeTitleInTooltip} />
				{/snippet}
				{@render menuEntry(
					item,
					collapsed || (shortcutsEnabled && !!item.shortcut?.length) ? simpleItemTooltipContent : undefined
				)}
			{/if}
		{/each}
	</Sidebar.Menu>
</Sidebar.Group>
