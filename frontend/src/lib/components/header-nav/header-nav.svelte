<script lang="ts">
	import {
		navigationItems,
		getManagementItems,
		getSwarmNavigationItems,
		filterByPermissions,
		type NavigationItem
	} from '$lib/config/navigation-config';
	import HeaderNavItem from './header-nav-item.svelte';
	import HeaderNavDropdown from './header-nav-dropdown.svelte';
	import HeaderNavOverflow from './header-nav-overflow.svelte';
	import HeaderEnvSwitcher from './header-env-switcher.svelte';
	import HeaderUserMenu from './header-user-menu.svelte';
	import HeaderUpdateBadge from './header-update-badge.svelte';
	import HeaderActivityPopover from './header-activity-popover.svelte';
	import VersionInfoDialog from '$lib/components/dialogs/version-info-dialog.svelte';
	import { NavOverflow, observeWidth } from './overflow.svelte';
	import type { PermissionsManifest, User } from '$lib/types/auth';
	import type { AppVersionInformation } from '$lib/types/settings';
	import userStore from '$lib/stores/user-store';
	import settingsStore from '$lib/stores/config-store';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { fromStore } from 'svelte/store';
	import { m } from '$lib/paraglide/messages';
	import { getApplicationLogo } from '$lib/utils/docker';
	import { accentColorPreviewStore } from '$lib/utils/theme';
	import { DockIcon } from '$lib/icons';

	let {
		user,
		versionInformation,
		swarmEnabled = false,
		permissionsManifest = null
	}: {
		versionInformation: AppVersionInformation;
		user?: User | null;
		swarmEnabled?: boolean;
		permissionsManifest?: PermissionsManifest | null;
	} = $props();

	const autoLogin = fromStore(settingsStore.autoLoginEnabled);
	const autoLoginEnabled = $derived(autoLogin.current);

	const storeUser = fromStore(userStore);
	const effectiveUser = $derived(storeUser.current ?? user);

	let showVersionDialog = $state(false);

	const accentColor = $derived($accentColorPreviewStore);
	const logoUrl = $derived(getApplicationLogo(false, accentColor, accentColor));

	const currentEnvId = $derived(environmentStore.selected?.id || '0');

	const entries = $derived.by<NavigationItem[]>(() => {
		const u = effectiveUser ?? null;
		const management = filterByPermissions(getManagementItems(currentEnvId), u, currentEnvId, permissionsManifest);
		const resources = filterByPermissions(navigationItems.resourceItems, u, currentEnvId, permissionsManifest);
		const swarm = filterByPermissions(getSwarmNavigationItems(swarmEnabled), u, currentEnvId, permissionsManifest);
		const settings = filterByPermissions(navigationItems.settingsItems, u, currentEnvId, permissionsManifest);
		const out: NavigationItem[] = [...management, ...resources];
		if (swarm.length > 0) {
			out.push({ title: m.swarm_title(), url: '/swarm', icon: DockIcon, items: swarm });
		}
		out.push(...settings);
		return out;
	});

	const overflow = new NavOverflow();
	$effect(() => {
		overflow.setItemCount(entries.length);
	});

	const visibleEntries = $derived(entries.slice(0, overflow.visibleCount));
	const overflowEntries = $derived(entries.slice(overflow.visibleCount));

	function isSyntheticParent(item: NavigationItem): boolean {
		return item.url === '/swarm';
	}
</script>

<VersionInfoDialog
	bind:open={showVersionDialog}
	onOpenChange={(open) => (showVersionDialog = open)}
	versionInfo={versionInformation}
	debugMode={false}
/>

<header class="sticky top-0 z-[var(--arcane-z-app-chrome)] w-full shrink-0 p-2">
	<div
		class="border-sidebar-border bg-background/60 flex h-13 items-center gap-2 rounded-lg border px-2.5 shadow-sm backdrop-blur-md"
	>
		<a href="/dashboard" class="flex shrink-0 items-center px-1.5" aria-label={m.layout_title()}>
			<img src={logoUrl} alt={m.layout_title()} class="h-6 w-6 drop-shadow-sm" width="24" height="24" />
		</a>

		<nav
			class="relative min-w-0 flex-1"
			aria-label={m.layout_title()}
			{@attach observeWidth((w) => (overflow.containerWidth = w))}
		>
			<!-- hidden replica row: measures the natural width of every item -->
			<div class="invisible absolute inset-x-0 top-0 flex items-center gap-1 overflow-hidden" aria-hidden="true">
				{#each entries as item, index (item.url)}
					<div {@attach observeWidth((w) => (overflow.itemWidths[index] = w))}>
						{#if (item.items?.length ?? 0) > 0}
							<HeaderNavDropdown {item} measureOnly />
						{:else}
							<HeaderNavItem {item} measureOnly />
						{/if}
					</div>
				{/each}
			</div>

			<div class="flex items-center gap-1">
				{#each visibleEntries as item (item.url)}
					{#if (item.items?.length ?? 0) > 0}
						<HeaderNavDropdown {item} parentLink={!isSyntheticParent(item)} />
					{:else}
						<HeaderNavItem {item} />
					{/if}
				{/each}
				{#if overflowEntries.length > 0}
					<HeaderNavOverflow items={overflowEntries} />
				{/if}
			</div>
		</nav>

		<div class="flex shrink-0 items-center gap-1">
			<HeaderEnvSwitcher />
			<HeaderActivityPopover />
			<HeaderUpdateBadge {versionInformation} debug={false} />
			{#if effectiveUser}
				<HeaderUserMenu
					user={effectiveUser}
					{autoLoginEnabled}
					versionLabel={m.sidebar_version({
						version: versionInformation?.displayVersion ?? versionInformation?.currentVersion ?? m.common_unknown()
					})}
					onShowVersion={() => (showVersionDialog = true)}
				/>
			{/if}
		</div>
	</div>
</header>
