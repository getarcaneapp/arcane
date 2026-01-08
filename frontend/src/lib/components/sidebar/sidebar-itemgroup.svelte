<script lang="ts">
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import * as Sidebar from '$lib/components/ui/sidebar/index.js';
	import { page } from '$app/state';
	import { useSidebar } from '$lib/components/ui/sidebar/context.svelte.js';
	import { ArrowRightIcon } from '$lib/icons';

	let {
		items,
		label
	}: {
		label: string;
		items: {
			title: string;
			url: string;
			icon?: typeof ArrowRightIcon;
			items?: {
				title: string;
				url: string;
				icon?: typeof ArrowRightIcon;
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
</script>

<Sidebar.Group>
	<Sidebar.GroupLabel>{label}</Sidebar.GroupLabel>
	<Sidebar.Menu>
		{#each enhancedItems as item (item.title)}
			{#if (item.items?.length ?? 0) > 0}
				{#if sidebar.state === 'collapsed' && !sidebar.hoverExpansionEnabled}
					<!-- In collapsed mode without hover expansion, show parent and children as separate icon buttons -->
					<Sidebar.MenuItem>
						<Sidebar.MenuButton isActive={item.isActive} tooltipContent={sidebar.hoverExpansionEnabled ? undefined : item.title}>
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
						<Sidebar.MenuItem>
							<Sidebar.MenuButton
								isActive={subItem.isActive}
								tooltipContent={sidebar.hoverExpansionEnabled ? undefined : subItem.title}
							>
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
					<Collapsible.Root
						open={getIsOpen(item.title, item.isActive)}
						onOpenChange={(open) => {
							if (!sidebar.hoverExpansionEnabled) {
								openStates[item.title] = open;
							}
						}}
						class="group/collapsible"
					>
						{#snippet child({ props })}
							<Sidebar.MenuItem {...props}>
								<Collapsible.Trigger>
									{#snippet child({ props })}
										{@const Icon = item.icon}
										<Sidebar.MenuButton
											tooltipContent={sidebar.hoverExpansionEnabled ? undefined : item.title}
											isActive={item.isActive}
										>
											{#snippet child({ props })}
												<a href={item.url} {...props}>
													{#if item.icon}
														<Icon />
													{/if}
													<span>{item.title}</span>
													<ArrowRightIcon
														class="ml-auto transition-transform duration-200 group-data-[state=open]/collapsible:rotate-90"
													/>
												</a>
											{/snippet}
										</Sidebar.MenuButton>
									{/snippet}
								</Collapsible.Trigger>
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
							</Sidebar.MenuItem>
						{/snippet}
					</Collapsible.Root>
				{/if}
			{:else}
				<Sidebar.MenuItem>
					<Sidebar.MenuButton isActive={item.isActive} tooltipContent={sidebar.hoverExpansionEnabled ? undefined : item.title}>
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
