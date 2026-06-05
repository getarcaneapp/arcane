<script lang="ts">
	import { navigationItems, getManagementItems, filterByPermissions } from '$lib/config/navigation-config';
	import type { NavigationItem } from '$lib/config/navigation-config';
	import { cn } from '$lib/utils';
	import { page } from '$app/state';
	import userStore from '$lib/stores/user-store';
	import { m } from '$lib/paraglide/messages';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import MobileUserCard from './mobile-user-card.svelte';
	import ActivityCenterTrigger from '$lib/components/activity/activity-center-trigger.svelte';
	import * as Drawer from '$lib/components/ui/drawer/index.js';
	import UpdateCenterDialog from '$lib/components/dialogs/update-center-dialog.svelte';
	import { useUpgradeCheck } from '$lib/hooks/use-upgrade-check.svelte';
	import type { AppVersionInformation } from '$lib/types/settings';
	import type { PermissionsManifest, User } from '$lib/types/auth';

	let {
		open = $bindable(false),
		user = null,
		versionInformation,
		swarmItems = [],
		permissionsManifest = null,
		debug = false
	}: {
		open: boolean;
		user?: User | null;
		versionInformation?: AppVersionInformation;
		swarmItems?: NavigationItem[];
		permissionsManifest?: PermissionsManifest | null;
		debug?: boolean;
	} = $props();

	let storeUser = $state<User | null>(null);

	$effect(() => {
		const unsub = userStore.subscribe((u) => (storeUser = u));
		return unsub;
	});

	const currentPath = $derived(page.url.pathname);
	const memoizedUser = $derived.by(() => user ?? storeUser);
	const currentEnvId = $derived(environmentStore.selected?.id || '0');
	const managementItemsRaw = $derived(getManagementItems(currentEnvId));
	const managementItems = $derived(
		filterByPermissions(managementItemsRaw, memoizedUser ?? null, currentEnvId, permissionsManifest)
	);
	const resourceItems = $derived(
		filterByPermissions(navigationItems.resourceItems, memoizedUser ?? null, currentEnvId, permissionsManifest)
	);
	const settingsItems = $derived(
		filterByPermissions(navigationItems.settingsItems, memoizedUser ?? null, currentEnvId, permissionsManifest)
	);

	const upgradeCheck = useUpgradeCheck({
		queryScope: 'mobile-nav',
		getVersionInformation: () => versionInformation,
		getDebug: () => debug
	});

	function handleItemClick() {
		open = false;
	}

	function isActiveItem(item: NavigationItem): boolean {
		return currentPath === item.url || currentPath.startsWith(item.url + '/');
	}
</script>

<Drawer.Root {open} onOpenChange={(nextOpen) => (open = nextOpen)} shouldScaleBackground direction="bottom" modal={true}>
	<Drawer.Overlay class="fixed inset-0 z-40 bg-black/40 backdrop-blur-xl" />
	<Drawer.Content
		data-testid="mobile-nav-sheet"
		class={cn('bg-background/95 rounded-t-3xl border border-t shadow-sm backdrop-blur-md', 'z-50 flex max-h-[85vh] flex-col')}
	>
		<div class="px-6 pt-4">
			{#if memoizedUser}
				<MobileUserCard user={memoizedUser} class="mb-6" />
			{/if}
			<ActivityCenterTrigger mobile class="mb-4" onOpen={handleItemClick} />
		</div>

		<div class="scrollbar-hide flex-1 overflow-y-auto px-6">
			<div class="space-y-8">
				<section>
					<h4 class="text-muted-foreground/70 mb-4 px-3 text-[11px] font-semibold tracking-widest uppercase">
						{m.sidebar_management()}
					</h4>
					<div class="space-y-2">
						{#each managementItems as item (item.url)}
							{#if item.items}
								{@const IconComponent = item.icon}
								<div class="space-y-2">
									<a
										href={item.url}
										onclick={handleItemClick}
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
									<div class="ml-6 space-y-1">
										{#each item.items as subItem (subItem.url)}
											{@const SubIconComponent = subItem.icon}
											<a
												href={subItem.url}
												onclick={handleItemClick}
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
									onclick={handleItemClick}
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
							{/if}
						{/each}
					</div>
				</section>

				<section>
					<h4 class="text-muted-foreground/70 mb-4 px-3 text-[11px] font-semibold tracking-widest uppercase">
						{m.sidebar_resources()}
					</h4>
					<div class="space-y-2">
						{#each resourceItems as item (item.url)}
							{#if item.items}
								{@const IconComponent = item.icon}
								<div class="space-y-2">
									<a
										href={item.url}
										onclick={handleItemClick}
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
									<div class="ml-6 space-y-1">
										{#each item.items as subItem (subItem.url)}
											{@const SubIconComponent = subItem.icon}
											<a
												href={subItem.url}
												onclick={handleItemClick}
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
									onclick={handleItemClick}
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
							{/if}
						{/each}
					</div>
				</section>

				{#if swarmItems.length > 0}
					<section>
						<h4 class="text-muted-foreground/70 mb-4 px-3 text-[11px] font-semibold tracking-widest uppercase">
							{m.swarm_title()}
						</h4>
						<div class="space-y-2">
							{#each swarmItems as item (item.url)}
								{@const IconComponent = item.icon}
								<a
									href={item.url}
									onclick={handleItemClick}
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
				{/if}

				{#if settingsItems.length > 0}
					<section>
						<h4 class="text-muted-foreground/70 mb-4 px-3 text-[11px] font-semibold tracking-widest uppercase">
							{m.sidebar_administration()}
						</h4>
						<div class="space-y-2">
							{#each settingsItems as item (item.url)}
								{#if item.items}
									{@const IconComponent = item.icon}
									<div class="space-y-2">
										<a
											href={item.url}
											onclick={handleItemClick}
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
											{#each item.items as subItem (subItem.url)}
												{@const SubIconComponent = subItem.icon}
												<a
													href={subItem.url}
													onclick={handleItemClick}
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
										onclick={handleItemClick}
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
			</div>
		</div>

		<div class="border-border/30 border-t px-6 pt-4 pb-4">
			{#if versionInformation}
				<div class="text-muted-foreground/60 text-center text-xs">
					<p class="font-medium">
						{m.layout_title()}
						{versionInformation.displayVersion ?? versionInformation.currentVersion}
					</p>
				</div>
				{#if upgradeCheck.shouldShowBanner}
					<button
						type="button"
						onclick={upgradeCheck.openDialog}
						disabled={upgradeCheck.upgrading || upgradeCheck.checkingUpgrade}
						class="group hover:bg-muted/50 focus-visible:ring-primary/40 mt-3 flex w-full items-center gap-2.5 rounded-xl px-3 py-2.5 text-left transition-colors focus-visible:ring-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60"
					>
						<span class="relative flex size-2 shrink-0 items-center justify-center">
							<span class="absolute inline-flex size-2 animate-ping rounded-full bg-blue-500 opacity-60"></span>
							<span class="relative inline-flex size-1.5 rounded-full bg-blue-500"></span>
						</span>
						<span class="text-foreground flex-1 text-sm font-medium">
							{m.sidebar_update_available()}
						</span>
						{#if upgradeCheck.versionChip}
							<span class="bg-muted text-muted-foreground rounded-md px-1.5 py-0.5 font-mono text-[11px]">
								{upgradeCheck.versionChip}
							</span>
						{/if}
					</button>
				{/if}
			{/if}
		</div>
	</Drawer.Content>
</Drawer.Root>

<UpdateCenterDialog
	bind:open={upgradeCheck.showConfirmDialog}
	bind:upgrading={upgradeCheck.upgrading}
	{versionInformation}
	canInstall={upgradeCheck.shouldShowUpgrade}
	{debug}
	onConfirm={upgradeCheck.confirmUpgrade}
/>

<style>
	:global(.scrollbar-hide) {
		-ms-overflow-style: none; /* IE and Edge */
		scrollbar-width: none; /* Firefox */
	}

	:global(.scrollbar-hide::-webkit-scrollbar) {
		display: none; /* Chrome, Safari and Opera */
	}
</style>
