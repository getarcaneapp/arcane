<script lang="ts">
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import * as Sidebar from '$lib/components/ui/sidebar/index.js';
	import { page } from '$app/state';
	import { useSidebar } from '$lib/components/ui/sidebar/context.svelte.js';
	import type { ShortcutKey } from '$lib/utils/keyboard-shortcut.utils';
	import { ArrowRightIcon } from '$lib/icons';
	import SidebarCollapsibleItem from './sidebar-collapsible-item.svelte';
	import SidebarItemTooltipContent from './sidebar-item-tooltip-content.svelte';

	let {
		items,
		label
	}: {
		label: string;
		items: {
			title: string;
			url: string;
			icon?: typeof ArrowRightIcon;
			shortcut?: ShortcutKey[];
			items?: {
				title: string;
				url: string;
				icon?: typeof ArrowRightIcon;
				shortcut?: ShortcutKey[];
			}[];
		}[];
	} = $props();

	const sidebar = useSidebar();

	function isActiveItem(url: string): boolean {
		// Special case: Don't highlight "Environments" when on GitOps page
		if (url === '/environments' && page.url.pathname.includes('/gitops')) {
			return false;
		}
		return page.url.pathname === url || (page.url.pathname.startsWith(url) && url !== '/');
	}

	function hasActiveChild(items?: { url: string }[]): boolean {
		return items?.some((child) => isActiveItem(child.url)) ?? false;
	}

	let openStates = $state<Record<string, boolean>>({});

	const enhancedItems = $derived(
		items.map((item) => {
			const isItemActive = isActiveItem(item.url);
			const hasActiveSubItem = hasActiveChild(item.items);
			const isActive = isItemActive || hasActiveSubItem;

			return {
				...item,
				isActive,
				items: item.items?.map((subItem) => ({
					...subItem,
					isActive: isActiveItem(subItem.url)
				}))
			};
		})
	);

	function getIsOpen(itemTitle: string, isActive: boolean): boolean {
		if (sidebar.hoverExpansionEnabled) {
			return isActive;
		}
		if (openStates[itemTitle] === undefined) {
			return isActive;
		}
		return openStates[itemTitle];
	}

	const collapsed = $derived(sidebar.state === 'collapsed');
	const includeTitleInTooltip = $derived(collapsed && !(sidebar.hoverExpansionEnabled && sidebar.isHovered));
</script>

<Sidebar.Group>
	<Sidebar.GroupLabel>{label}</Sidebar.GroupLabel>
	<Sidebar.Menu>
		{#each enhancedItems as item (item.title)}
			{#if (item.items?.length ?? 0) > 0}
				{#if sidebar.state === 'collapsed' && !sidebar.hoverExpansionEnabled}
					<!-- In collapsed mode without hover expansion, show parent and children as separate icon buttons -->
					{#snippet tooltipContent()}
						<SidebarItemTooltipContent title={item.title} shortcut={item.shortcut} includeTitle={true} />
					{/snippet}
					<Sidebar.MenuItem>
						<Sidebar.MenuButton isActive={item.isActive} {tooltipContent}>
							{#snippet child({ props })}
								{@const Icon = item.icon}
								<a href={item.url} {...props}>
									{#if item.icon}
										<Icon />
									{/if}
									<span>{item.title}</span>
								</a>
							{/snippet}
						</Sidebar.MenuButton>
					</Sidebar.MenuItem>
					<!-- Separator before sub-items -->
					<div class="flex justify-center px-2 py-1">
						<Sidebar.Separator class="my-0 w-6" />
					</div>
					{#each item.items ?? [] as subItem (subItem.title)}
						{#snippet subItemTooltipContent()}
							<SidebarItemTooltipContent title={subItem.title} shortcut={subItem.shortcut} includeTitle={true} />
						{/snippet}
						<Sidebar.MenuItem>
							<Sidebar.MenuButton isActive={subItem.isActive} tooltipContent={subItemTooltipContent}>
								{#snippet child({ props })}
									{@const SubIcon = subItem.icon}
									<a href={subItem.url} {...props}>
										{#if subItem.icon}
											<SubIcon />
										{/if}
										<span>{subItem.title}</span>
									</a>
								{/snippet}
							</Sidebar.MenuButton>
						</Sidebar.MenuItem>
					{/each}
				{:else}
					{#snippet collapsibleSubMenu()}
						<Collapsible.Content>
							<Sidebar.MenuSub
								class={sidebar.state === 'collapsed' && (!sidebar.isHovered || !sidebar.hoverExpansionEnabled)
									? 'hidden'
									: undefined}
							>
								{#each item.items ?? [] as subItem (subItem.title)}
									<Sidebar.MenuSubItem>
										<Sidebar.MenuSubButton isActive={subItem.isActive}>
											{#snippet child({ props })}
												{@const SubIcon = subItem.icon}
												<a href={subItem.url} {...props}>
													{#if subItem.icon}
														<SubIcon />
													{/if}
													<span>{subItem.title}</span>
												</a>
											{/snippet}
										</Sidebar.MenuSubButton>
									</Sidebar.MenuSubItem>
								{/each}
							</Sidebar.MenuSub>
						</Collapsible.Content>
					{/snippet}
					<SidebarCollapsibleItem
						{item}
						showTooltip={collapsed || !!item.shortcut?.length}
						{includeTitleInTooltip}
						{getIsOpen}
						onOpenChange={(open) => {
							if (!sidebar.hoverExpansionEnabled) {
								openStates[item.title] = open;
							}
						}}
						content={collapsibleSubMenu}
					/>
				{/if}
			{:else}
				{#snippet simpleItemTooltipContent()}
					<SidebarItemTooltipContent title={item.title} shortcut={item.shortcut} includeTitle={includeTitleInTooltip} />
				{/snippet}
				<Sidebar.MenuItem>
					<Sidebar.MenuButton
						isActive={item.isActive}
						tooltipContent={collapsed || !!item.shortcut?.length ? simpleItemTooltipContent : undefined}
					>
						{#snippet child({ props })}
							{@const Icon = item.icon}
							<a href={item.url} {...props}>
								{#if item.icon}
									<Icon />
								{/if}
								<span>{item.title}</span>
							</a>
						{/snippet}
					</Sidebar.MenuButton>
				</Sidebar.MenuItem>
			{/if}
		{/each}
	</Sidebar.Menu>
</Sidebar.Group>
