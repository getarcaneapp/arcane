<script lang="ts">
	import { navigationItems } from '$lib/config/navigation-config';
	import type { NavigationItem } from '$lib/config/navigation-config';
	import { cn } from '$lib/utils';
	import { page } from '$app/state';
	import userStore from '$lib/stores/user-store';
	import { m } from '$lib/paraglide/messages';
	import MobileUserCard from './mobile-user-card.svelte';
	import * as Drawer from '$lib/components/ui/drawer/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { queryKeys } from '$lib/query/query-keys';
	import systemUpgradeService from '$lib/services/api/system-upgrade-service';
	import UpgradeConfirmationDialog from '$lib/components/dialogs/upgrade-confirmation-dialog.svelte';
	import { toast } from 'svelte-sonner';
	import { DownloadIcon } from '$lib/icons';
	import { extractApiErrorMessage } from '$lib/utils/api.util';
	import type { AppVersionInformation } from '$lib/types/application-configuration';
	import { createMutation, createQuery } from '@tanstack/svelte-query';

	let {
		open = $bindable(false),
		user = null,
		versionInformation,
		swarmEnabled = false,
		debug = false
	}: {
		open: boolean;
		user?: any;
		versionInformation?: AppVersionInformation;
		swarmEnabled?: boolean;
		debug?: boolean;
	} = $props();

	let storeUser: any = $state(null);

	$effect(() => {
		const unsub = userStore.subscribe((u) => (storeUser = u));
		return unsub;
	});

	const currentPath = $derived(page.url.pathname);
	const memoizedUser = $derived.by(() => user ?? storeUser);

	let upgrading = $state(false);
	let showConfirmDialog = $state(false);
	const isAdmin = $derived(!!user?.roles?.includes('admin'));

	const shouldCheckUpgrade = $derived(!!(versionInformation?.updateAvailable && isAdmin && !debug));
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
	const shouldShowUpgrade = $derived((canUpgrade && isAdmin) || debug);

	// Determine update type and display text
	const updateType = $derived.by(() => {
		if (!versionInformation) return 'none';
		if (versionInformation.isSemverVersion) return 'semver';
		if (versionInformation.currentTag && versionInformation.newestDigest) return 'digest';
		return 'none';
	});

	const updateDisplayText = $derived.by(() => {
		if (!versionInformation) return '';
		if (updateType === 'semver') {
			return versionInformation.newestVersion ?? '';
		}
		if (updateType === 'digest' && versionInformation.newestDigest) {
			// Show shortened digest for non-semver tags
			const digest = versionInformation.newestDigest;
			return digest.length > 12 ? digest.substring(0, 12) : digest;
		}
		return '';
	});

	const upgradeButtonText = $derived.by(() => {
		if (upgrading) return m.upgrade_in_progress();
		if (checkingUpgrade) return m.upgrade_checking();
		if (updateType === 'digest') {
			const tag = versionInformation?.currentTag ?? m.common_image();
			return m.upgrade_update_tag({ tag });
		}
		// For semver updates, use newestVersion or fallback to updateDisplayText
		const version = versionInformation?.newestVersion || updateDisplayText;
		if (version) {
			return m.upgrade_to_version({ version });
		}
		// Fallback if no version info is available (shouldn't happen in prod, but safe fallback)
		return m.upgrade_now();
	});

	// Show banner for both semver and digest-based updates
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
						{#each navigationItems.managementItems as item (item.url)}
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

				<section>
					<h4 class="text-muted-foreground/70 mb-4 px-3 text-[11px] font-bold tracking-widest uppercase">
						{m.sidebar_resources()}
					</h4>
					<div class="space-y-2">
						{#each navigationItems.resourceItems as item (item.url)}
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

				<section>
					<h4 class="text-muted-foreground/70 mb-4 px-3 text-[11px] font-bold tracking-widest uppercase">
						{m.security_title()}
					</h4>
					<div class="space-y-2">
						{#each navigationItems.securityItems as item (item.url)}
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

				{#if swarmEnabled}
					<section>
						<h4 class="text-muted-foreground/70 mb-4 px-3 text-[11px] font-bold tracking-widest uppercase">
							{m.swarm_title()}
						</h4>
						<div class="space-y-2">
							{#each navigationItems.swarmItems as item}
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

				{#if isAdmin}
					{#if navigationItems.settingsItems}
						<section>
							<h4 class="text-muted-foreground/70 mb-4 px-3 text-[11px] font-bold tracking-widest uppercase">
								{m.sidebar_administration()}
							</h4>
							<div class="space-y-2">
								{#each navigationItems.settingsItems as item (item.url)}
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
				{/if}
			</div>
		</div>

		<div class="border-border/30 border-t px-6 pt-4 pb-4">
			{#if versionInformation}
				<div class="text-muted-foreground/60 text-center text-xs">
					<p class="font-medium">
						Arcane {versionInformation.displayVersion ?? versionInformation.currentVersion}
					</p>
					{#if shouldShowBanner}
						<p class="text-primary/80 mt-1 font-medium">Update available</p>
					{/if}
				</div>
				{#if shouldShowUpgrade}
					<div class="mt-3">
						<ArcaneButton
							action="update"
							size="sm"
							class="h-9 w-full gap-2 rounded-xl shadow-sm transition-colors"
							onclick={handleUpgradeClick}
							disabled={upgrading || checkingUpgrade}
							customLabel={upgradeButtonText}
							icon={DownloadIcon}
						/>
					</div>
				{/if}
			{/if}
		</div>
	</Drawer.Content>
</Drawer.Root>

<UpgradeConfirmationDialog
	bind:open={showConfirmDialog}
	bind:upgrading
	version={versionInformation?.newestVersion ?? ''}
	expectedVersion={versionInformation?.newestVersion}
	expectedDigest={versionInformation?.newestDigest}
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
