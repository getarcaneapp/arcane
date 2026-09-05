<script lang="ts">
	import { navigationItems, getSetupItems, filterByPermissions } from '#lib/config/navigation-config';
	import type { NavigationItem } from '#lib/config/navigation-config';
	import { cn } from '#lib/utils';
	import { page } from '$app/state';
	import userStore from '#lib/stores/user-store';
	import { m } from '#lib/paraglide/messages';
	import { environmentStore } from '#lib/stores/environment.store.svelte';
	import MobileUserCard from './mobile-user-card.svelte';
	import ActivityCenterTrigger from '#lib/components/activity/activity-center-trigger.svelte';
	import * as Drawer from '#lib/components/ui/drawer/index.js';
	import UpdateAllDialog from '#lib/components/dialogs/update-all-dialog.svelte';
	import { useUpgradeCheck } from '#lib/hooks/use-upgrade-check.svelte';
	import UpdateAvailableBanner from '#lib/components/sidebar/update-available-banner.svelte';
	import type { AppVersionInformation } from '#lib/types/settings';
	import type { PermissionsManifest, User } from '#lib/types/auth';

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
	const setupItemsRaw = $derived(getSetupItems(currentEnvId));
	const managementItems = $derived(
		filterByPermissions(navigationItems.managementItems, memoizedUser ?? null, currentEnvId, permissionsManifest)
	);
	const resourceItems = $derived(
		filterByPermissions(navigationItems.resourceItems, memoizedUser ?? null, currentEnvId, permissionsManifest)
	);
	const settingsItems = $derived(filterByPermissions(setupItemsRaw, memoizedUser ?? null, currentEnvId, permissionsManifest));

	const permittedSwarmItems = $derived(filterByPermissions(swarmItems, memoizedUser ?? null, currentEnvId, permissionsManifest));

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

{#snippet navLink(item: NavigationItem, focusStyles: boolean)}
	{@const IconComponent = item.icon}
	<a
		href={item.url}
		onclick={handleItemClick}
		class={cn(
			'flex items-center gap-3 rounded-2xl px-4 py-3 text-sm font-medium transition-all duration-200 ease-out',
			focusStyles &&
				'hover:scale-[1.01] focus-visible:ring-1 focus-visible:ring-muted-foreground/50 focus-visible:ring-offset-1 focus-visible:ring-offset-transparent',
			isActiveItem(item) ? 'bg-muted text-foreground shadow-sm hover:bg-muted/70' : 'text-foreground hover:bg-muted/50'
		)}
		aria-current={focusStyles && isActiveItem(item) ? 'page' : undefined}
	>
		<IconComponent size={20} />
		<span>{item.title}</span>
	</a>
{/snippet}

{#snippet subLinks(subItems: NavigationItem[])}
	<div class="ml-6 space-y-1">
		{#each subItems as subItem (subItem.url)}
			{@const SubIconComponent = subItem.icon}
			<a
				href={subItem.url}
				onclick={handleItemClick}
				class={cn(
					'flex items-center gap-3 rounded-xl px-4 py-2 text-sm transition-all duration-200 ease-out',
					'hover:scale-[1.01] focus-visible:ring-1 focus-visible:ring-muted-foreground/50 focus-visible:ring-offset-1 focus-visible:ring-offset-transparent',
					isActiveItem(subItem)
						? 'bg-muted/70 text-foreground shadow-sm'
						: 'text-muted-foreground hover:bg-muted/40 hover:text-foreground'
				)}
				aria-current={isActiveItem(subItem) ? 'page' : undefined}
			>
				<SubIconComponent size={16} />
				<span>{subItem.title}</span>
			</a>
		{/each}
	</div>
{/snippet}

{#snippet navItems(sectionItems: NavigationItem[], focusStyles: boolean)}
	<div class="space-y-2">
		{#each sectionItems as item (item.url)}
			{#if item.items}
				<div class="space-y-2">
					{@render navLink(item, focusStyles)}
					{@render subLinks(item.items)}
				</div>
			{:else}
				{@render navLink(item, focusStyles)}
			{/if}
		{/each}
	</div>
{/snippet}

{#snippet navSection(label: string, items: NavigationItem[])}
	{#if items.length > 0}
		<section aria-label={label}>
			<h4 class="mb-4 px-3 text-[11px] font-semibold tracking-widest text-muted-foreground/70 uppercase">{label}</h4>
			{@render navItems(items, true)}
		</section>
	{/if}
{/snippet}

<Drawer.Root {open} onOpenChange={(nextOpen) => (open = nextOpen)} shouldScaleBackground direction="bottom" modal={true}>
	<Drawer.Overlay class="fixed inset-0 z-[var(--arcane-z-overlay)] bg-black/40 backdrop-blur-xl" />
	<Drawer.Content
		data-testid="mobile-nav-sheet"
		class={cn(
			'rounded-t-3xl border border-t bg-background/95 shadow-sm backdrop-blur-md',
			'z-[var(--arcane-z-surface)] flex max-h-[85vh] flex-col'
		)}
	>
		<div class="px-6 pt-4">
			{#if memoizedUser}
				<MobileUserCard user={memoizedUser} class="mb-6" />
			{/if}
			<ActivityCenterTrigger mobile class="mb-4" onOpen={handleItemClick} />
		</div>

		<div class="scrollbar-hide flex-1 overflow-y-auto px-6">
			<div class="space-y-8">
				{@render navSection(m.common_overview(), managementItems)}
				{@render navSection(m.workloads(), resourceItems)}
				{@render navSection(m.setup(), settingsItems)}
				{@render navSection(m.swarm(), permittedSwarmItems)}
			</div>
		</div>

		<div class="border-t border-border/30 px-6 pt-4 pb-4">
			{#if versionInformation}
				<div class="text-center text-xs text-muted-foreground/60">
					<p class="font-medium">
						{m.layout_title()}
						{versionInformation.displayVersion ?? versionInformation.currentVersion}
					</p>
				</div>
				{#if upgradeCheck.shouldShowBanner}
					<UpdateAvailableBanner
						class="mt-3 rounded-xl px-3 py-2.5"
						label={m.sidebar_update_available()}
						versionChip={upgradeCheck.versionChip}
						disabled={upgradeCheck.checkingUpgrade}
						onclick={upgradeCheck.openDialog}
					/>
				{/if}
			{/if}
		</div>
	</Drawer.Content>
</Drawer.Root>

<UpdateAllDialog
	bind:open={upgradeCheck.showConfirmDialog}
	{versionInformation}
	canConfirm={upgradeCheck.shouldShowUpgrade}
	debugDemo={debug}
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
