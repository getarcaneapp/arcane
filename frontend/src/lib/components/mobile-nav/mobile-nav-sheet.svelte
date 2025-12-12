<script lang="ts">
	import { navigationItems } from '$lib/config/navigation-config';
	import type { NavigationItem } from '$lib/config/navigation-config';
	import { cn } from '$lib/utils';
	import { page } from '$app/state';
	import userStore from '$lib/stores/user-store';
	import { m } from '$lib/paraglide/messages';
	import MobileUserCard from './mobile-user-card.svelte';
	import * as Drawer from '$lib/components/ui/drawer/index.js';

	let {
		open = $bindable(false),
		user = null,
		versionInformation = null,
		navigationMode = 'floating'
	}: {
		open: boolean;
		user?: any;
		versionInformation?: any;
		navigationMode?: 'floating' | 'docked';
	} = $props();

	let storeUser: any = $state(null);

	$effect(() => {
		const unsub = userStore.subscribe((u) => (storeUser = u));
		return unsub;
	});

	const currentPath = $derived(page.url.pathname);
	const memoizedUser = $derived.by(() => user ?? storeUser);
	const memoizedIsAdmin = $derived.by(() => !!memoizedUser?.roles?.includes('admin'));

	function handleItemClick(item: NavigationItem, event?: MouseEvent) {
		// Don't prevent default - let the navigation happen
		open = false;
	}

	function isActiveItem(item: NavigationItem): boolean {
		return currentPath === item.url || currentPath.startsWith(item.url + '/');
	}
</script>

<Drawer.Root bind:open shouldScaleBackground direction="bottom" modal={true}>
	<Drawer.Overlay class="fixed inset-0 z-40 bg-black/40 backdrop-blur-xl" />
	<Drawer.Content
		data-testid="mobile-nav-sheet"
		class={cn('bg-background/95 rounded-t-3xl border border-t shadow-sm backdrop-blur-md', 'z-50 flex max-h-[85vh] flex-col')}
	>
		<div class="px-6 pt-4">
			{#if memoizedUser}
				<MobileUserCard user={memoizedUser} class="mb-6" />
			{/if}
		</div>

		<div class="scrollbar-hide flex-1 overflow-y-auto px-6">
			<div class="space-y-8">
				<section>
					<h4 class="text-muted-foreground/70 mb-4 px-3 text-[11px] font-bold tracking-widest uppercase">
						{m.sidebar_management()}
					</h4>
					<div class="space-y-2">
						{#each navigationItems.managementItems as item}
							{@const IconComponent = item.icon}
							<a
								href={item.url}
								onclick={() => handleItemClick(item)}
								class={cn(
									'flex items-center gap-3 rounded-2xl px-4 py-3 text-sm font-medium transition-all duration-200 ease-out',
									'focus-visible:ring-muted-foreground/50 hover:scale-[1.01] focus-visible:ring-1 focus-visible:ring-offset-1 focus-visible:ring-offset-transparent',
									isActiveItem(item)
										? 'bg-muted text-foreground hover:bg-muted/70 shadow-sm'
										: 'text-foreground hover:bg-muted/50'
								)}
								aria-current={isActiveItem(item) ? 'page' : undefined}
							>
								<IconComponent size={20} />
								<span>{item.title}</span>
							</a>
						{/each}
					</div>
				</section>

				<section>
					<h4 class="text-muted-foreground/70 mb-4 px-3 text-[11px] font-bold tracking-widest uppercase">
						{m.sidebar_resources()}
					</h4>
					<div class="space-y-2">
						{#each navigationItems.resourceItems as item}
							{@const IconComponent = item.icon}
							<a
								href={item.url}
								onclick={() => handleItemClick(item)}
								class={cn(
									'flex items-center gap-3 rounded-2xl px-4 py-3 text-sm font-medium transition-all duration-200 ease-out',
									'focus-visible:ring-muted-foreground/50 hover:scale-[1.01] focus-visible:ring-1 focus-visible:ring-offset-1 focus-visible:ring-offset-transparent',
									isActiveItem(item)
										? 'bg-muted text-foreground hover:bg-muted/70 shadow-sm'
										: 'text-foreground hover:bg-muted/50'
								)}
								aria-current={isActiveItem(item) ? 'page' : undefined}
							>
								<IconComponent size={20} />
								<span>{item.title}</span>
							</a>
						{/each}
					</div>
				</section>

				{#if memoizedIsAdmin}
					{#if navigationItems.settingsItems}
						<section>
							<h4 class="text-muted-foreground/70 mb-4 px-3 text-[11px] font-bold tracking-widest uppercase">
								{m.sidebar_administration()}
							</h4>
							<div class="space-y-2">
								{#each navigationItems.settingsItems as item}
									{#if item.items}
										{@const IconComponent = item.icon}
										<div class="space-y-2">
											<a
												href={item.url}
												onclick={() => handleItemClick(item)}
												class={cn(
													'flex items-center gap-3 rounded-2xl px-4 py-3 text-sm font-medium transition-all duration-200 ease-out',
													isActiveItem(item)
														? 'bg-muted text-foreground hover:bg-muted/70 shadow-sm'
														: 'text-foreground hover:bg-muted/50'
												)}
											>
												<IconComponent size={20} />
												<span>{item.title}</span>
											</a>
											<div class="ml-6 space-y-1">
												{#each item.items as subItem}
													{@const SubIconComponent = subItem.icon}
													<a
														href={subItem.url}
														onclick={() => handleItemClick(subItem)}
														class={cn(
															'flex items-center gap-3 rounded-xl px-4 py-2 text-sm transition-all duration-200 ease-out',
															'focus-visible:ring-muted-foreground/50 hover:scale-[1.01] focus-visible:ring-1 focus-visible:ring-offset-1 focus-visible:ring-offset-transparent',
															isActiveItem(subItem)
																? 'bg-muted/70 text-foreground shadow-sm'
																: 'text-muted-foreground hover:text-foreground hover:bg-muted/40'
														)}
														aria-current={isActiveItem(subItem) ? 'page' : undefined}
													>
														<SubIconComponent size={16} />
														<span>{subItem.title}</span>
													</a>
												{/each}
											</div>
										</div>
									{:else}
										{@const IconComponent = item.icon}
										<a
											href={item.url}
											onclick={() => handleItemClick(item)}
											class={cn(
												'flex items-center gap-3 rounded-2xl px-4 py-3 text-sm font-medium transition-all duration-200 ease-out',
												isActiveItem(item)
													? 'bg-muted text-foreground hover:bg-muted/70 shadow-sm'
													: 'text-foreground hover:bg-muted/50'
											)}
										>
											<IconComponent size={20} />
											<span>{item.title}</span>
										</a>
									{/if}
								{/each}
							</div>
						</section>
					{/if}
				{/if}
			</div>
		</div>

		<div class="border-border/30 border-t px-6 pt-4 pb-4">
			{#if versionInformation}
				<div class="text-muted-foreground/60 text-center text-xs">
					<p class="font-medium">Arcane v{versionInformation.currentVersion}</p>
					{#if versionInformation.updateAvailable}
						<p class="text-primary/80 mt-1 font-medium">Update available</p>
					{/if}
				</div>
			{/if}
		</div>
	</Drawer.Content>
</Drawer.Root>

<style>
	:global(.scrollbar-hide) {
		-ms-overflow-style: none; /* IE and Edge */
		scrollbar-width: none; /* Firefox */
	}

	:global(.scrollbar-hide::-webkit-scrollbar) {
		display: none; /* Chrome, Safari and Opera */
	}
</style>
