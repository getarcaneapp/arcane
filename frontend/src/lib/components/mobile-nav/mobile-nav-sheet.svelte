<script lang="ts">
	import { navigationItems, getManagementItems, filterByPermissions } from '$lib/config/navigation-config';
	import type { NavigationItem } from '$lib/config/navigation-config';
	import { cn } from '$lib/utils';
	import { page } from '$app/state';
	import userStore from '$lib/stores/user-store';
	import { m } from '$lib/paraglide/messages';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import MobileUserCard from './mobile-user-card.svelte';
	import * as Drawer from '$lib/components/ui/drawer/index.js';
	import { queryKeys } from '$lib/query/query-keys';
	import systemUpgradeService from '$lib/services/api/system-upgrade-service';
	import UpdateCenterDialog from '$lib/components/dialogs/update-center-dialog.svelte';
	import { toast } from 'svelte-sonner';
	import { extractApiErrorMessage } from '$lib/utils/api';
	import { hasPermission } from '$lib/utils/auth';
	import type { AppVersionInformation } from '$lib/types/settings';
	import { createMutation, createQuery } from '@tanstack/svelte-query';

	let {
		open = $bindable(false),
		user = null,
		versionInformation,
		swarmItems = [],
		debug = false
	}: {
		open: boolean;
		user?: any;
		versionInformation?: AppVersionInformation;
		swarmItems?: NavigationItem[];
		debug?: boolean;
	} = $props();

	let storeUser: any = $state(null);

	$effect(() => {
		const unsub = userStore.subscribe((u) => (storeUser = u));
		return unsub;
	});

	const currentPath = $derived(page.url.pathname);
	const memoizedUser = $derived.by(() => user ?? storeUser);
	const currentEnvId = $derived(environmentStore.selected?.id || '0');
	const managementItemsRaw = $derived(getManagementItems(currentEnvId));
	const managementItems = $derived(filterByPermissions(managementItemsRaw, memoizedUser ?? null, currentEnvId));
	const resourceItems = $derived(filterByPermissions(navigationItems.resourceItems, memoizedUser ?? null, currentEnvId));
	const settingsItems = $derived(filterByPermissions(navigationItems.settingsItems, memoizedUser ?? null, currentEnvId));

	let upgrading = $state(false);
	let showConfirmDialog = $state(false);
	const canInstallUpdates = $derived(hasPermission('environments:update'));

	const shouldCheckUpgrade = $derived(!!(versionInformation?.updateAvailable && canInstallUpdates && !debug));
	const upgradeAvailabilityQuery = createQuery(() => ({
		queryKey: queryKeys.system.upgradeAvailable('mobile-nav'),
		queryFn: () => systemUpgradeService.checkUpgradeAvailable(),
		enabled: shouldCheckUpgrade,
		staleTime: 0
	}));

	const canUpgrade = $derived.by(() => {
		if (debug) return true;
		const result = upgradeAvailabilityQuery.data;
		return !!result?.canUpgrade && !result?.error;
	});
	const checkingUpgrade = $derived(
		!!(shouldCheckUpgrade && (upgradeAvailabilityQuery.isPending || upgradeAvailabilityQuery.isFetching))
	);
	const shouldShowUpgrade = $derived((canUpgrade && canInstallUpdates) || debug);

	const updateType = $derived.by(() => {
		if (!versionInformation) return 'none';
		if (versionInformation.isSemverVersion) return 'semver';
		if (versionInformation.currentTag && versionInformation.newestDigest) return 'digest';
		return 'none';
	});

	const versionChip = $derived.by(() => {
		if (!versionInformation) return '';
		if (updateType === 'semver') return versionInformation.newestVersion ?? '';
		if (updateType === 'digest') return versionInformation.currentTag ?? '';
		return '';
	});

	const shouldShowBanner = $derived(versionInformation?.updateAvailable || debug);
	const triggerUpgradeMutation = createMutation(() => ({
		mutationFn: () => systemUpgradeService.triggerUpgrade(),
		onError: (error: unknown) => {
			const errorMessage = extractApiErrorMessage(error);
			const wrappedPrefix = m.upgrade_failed({ error: '' });
			toast.error(errorMessage.startsWith(wrappedPrefix) ? errorMessage : m.upgrade_failed({ error: errorMessage }));
			upgrading = false;
		}
	}));

	function handleUpgradeClick() {
		showConfirmDialog = true;
	}

	function handleConfirmUpgrade() {
		triggerUpgradeMutation.mutate();
	}

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
						Arcane {versionInformation.displayVersion ?? versionInformation.currentVersion}
					</p>
				</div>
				{#if shouldShowBanner}
					<button
						type="button"
						onclick={handleUpgradeClick}
						disabled={upgrading || checkingUpgrade}
						class="group hover:bg-muted/50 focus-visible:ring-primary/40 mt-3 flex w-full items-center gap-2.5 rounded-xl px-3 py-2.5 text-left transition-colors focus-visible:ring-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60"
					>
						<span class="relative flex size-2 shrink-0 items-center justify-center">
							<span class="absolute inline-flex size-2 animate-ping rounded-full bg-blue-500 opacity-60"></span>
							<span class="relative inline-flex size-1.5 rounded-full bg-blue-500"></span>
						</span>
						<span class="text-foreground flex-1 text-sm font-medium">
							{m.sidebar_update_available()}
						</span>
						{#if versionChip}
							<span class="bg-muted text-muted-foreground rounded-md px-1.5 py-0.5 font-mono text-[11px]">
								{versionChip}
							</span>
						{/if}
					</button>
				{/if}
			{/if}
		</div>
	</Drawer.Content>
</Drawer.Root>

<UpdateCenterDialog
	bind:open={showConfirmDialog}
	bind:upgrading
	{versionInformation}
	canInstall={shouldShowUpgrade}
	{debug}
	onConfirm={handleConfirmUpgrade}
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
