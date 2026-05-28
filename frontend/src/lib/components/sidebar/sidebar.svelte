<script lang="ts" module>
	import {
		navigationItems,
		getManagementItems,
		getSwarmNavigationItems,
		filterByPermissions
	} from '$lib/config/navigation-config';
</script>

<script lang="ts">
	import SidebarItemGroup from './sidebar-itemgroup.svelte';
	import SidebarUser from './sidebar-user.svelte';
	import SidebarEnvSwitcher from './sidebar-env-switcher.svelte';
	import EnvironmentSwitcherDialog from '$lib/components/dialogs/environment-switcher-dialog.svelte';
	import * as Sidebar from '$lib/components/ui/sidebar/index.js';
	import { useSidebar } from '$lib/components/ui/sidebar/index.js';
	import type { ComponentProps } from 'svelte';
	import type { User } from '$lib/types/auth';
	import type { AppVersionInformation } from '$lib/types/settings';
	import SidebarLogo from './sidebar-logo.svelte';
	import SidebarUpdatebanner from './sidebar-updatebanner.svelte';
	import SidebarPinButton from './sidebar-pin-button.svelte';
	import ActivityCenterTrigger from '$lib/components/activity/activity-center-trigger.svelte';
	import userStore from '$lib/stores/user-store';
	import settingsStore from '$lib/stores/config-store';
	import { m } from '$lib/paraglide/messages';
	import VersionInfoDialog from '$lib/components/dialogs/version-info-dialog.svelte';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { fromStore } from 'svelte/store';

	let {
		ref = $bindable(null),
		collapsible = 'icon',
		variant = 'floating',
		user,
		versionInformation,
		swarmEnabled = false,
		...restProps
	}: ComponentProps<typeof Sidebar.Root> & {
		versionInformation: AppVersionInformation;
		user?: User | null;
		swarmEnabled?: boolean;
	} = $props();

	let autoLoginEnabled = $state(false);
	$effect(() => {
		const unsub = settingsStore.autoLoginEnabled.subscribe((v) => (autoLoginEnabled = v));
		return unsub;
	});

	const sidebar = useSidebar();

	const storeUser = fromStore(userStore);
	let showVersionDialog = $state(false);
	const effectiveUser = $derived(user ?? storeUser.current);

	const isCollapsed = $derived(sidebar.state === 'collapsed' && !(sidebar.hoverExpansionEnabled && sidebar.isHovered));
	let envSwitcherOpen = $state(false);

	const currentEnvId = $derived(environmentStore.selected?.id || '0');
	const managementItemsRaw = $derived(getManagementItems(currentEnvId));
	const swarmItemsRaw = $derived(getSwarmNavigationItems(swarmEnabled));

	const managementItems = $derived(filterByPermissions(managementItemsRaw, effectiveUser ?? null, currentEnvId));
	const resourceItems = $derived(filterByPermissions(navigationItems.resourceItems, effectiveUser ?? null, currentEnvId));
	const swarmItems = $derived(filterByPermissions(swarmItemsRaw, effectiveUser ?? null, currentEnvId));
	const settingsItems = $derived(filterByPermissions(navigationItems.settingsItems, effectiveUser ?? null, currentEnvId));
</script>

<VersionInfoDialog
	bind:open={showVersionDialog}
	onOpenChange={(open) => (showVersionDialog = open)}
	versionInfo={versionInformation}
	debugMode={false}
/>

<EnvironmentSwitcherDialog bind:open={envSwitcherOpen} />

<Sidebar.Root {collapsible} {variant} {...restProps}>
	<Sidebar.Header class={isCollapsed ? 'gap-0 p-1 pb-2' : ''}>
		{#if isCollapsed}
			<div class="flex justify-center">
				<SidebarPinButton />
			</div>
		{/if}
		<div class="relative">
			<SidebarLogo {isCollapsed} />
			{#if !isCollapsed}
				<div class="absolute top-0 right-0 -mt-1 -mr-1">
					<SidebarPinButton />
				</div>
			{/if}
		</div>
		{#if isCollapsed}
			<div class="flex justify-center px-1">
				<SidebarEnvSwitcher onOpenDialog={() => (envSwitcherOpen = true)} />
			</div>
		{:else}
			<SidebarEnvSwitcher onOpenDialog={() => (envSwitcherOpen = true)} />
		{/if}
		{#if isCollapsed}
			<div class="flex justify-center px-1 pt-1">
				<ActivityCenterTrigger collapsed compact />
			</div>
		{:else}
			<Sidebar.Menu class="pt-1">
				<Sidebar.MenuItem>
					<ActivityCenterTrigger compact />
				</Sidebar.MenuItem>
			</Sidebar.Menu>
		{/if}
	</Sidebar.Header>
	<Sidebar.Content class={!isCollapsed ? '-mt-2' : ''}>
		{#if managementItems.length > 0}
			<SidebarItemGroup label={m.sidebar_management()} items={managementItems} />
		{/if}
		{#if resourceItems.length > 0}
			<SidebarItemGroup label={m.sidebar_resources()} items={resourceItems} />
		{/if}
		{#if swarmItems.length > 0}
			<SidebarItemGroup label={m.swarm_title()} items={swarmItems} />
		{/if}
		{#if settingsItems.length > 0}
			<SidebarItemGroup label={m.sidebar_administration()} items={settingsItems} />
		{/if}
	</Sidebar.Content>
	<Sidebar.Footer>
		<SidebarUpdatebanner {isCollapsed} {versionInformation} debug={false} />
		{#if effectiveUser}
			<div class={isCollapsed ? 'px-0 pb-2' : 'px-3 pb-2'}>
				<SidebarUser {isCollapsed} user={effectiveUser} {autoLoginEnabled} />
			</div>
		{/if}
		<div class={`flex items-center justify-center ${isCollapsed ? 'px-1' : 'px-4'}`}>
			<button
				type="button"
				onclick={() => (showVersionDialog = true)}
				class="text-muted-foreground/60 hover:text-muted-foreground cursor-pointer text-xs font-medium transition-colors"
			>
				{m.sidebar_version({
					version: versionInformation?.displayVersion ?? versionInformation?.currentVersion ?? m.common_unknown()
				})}
			</button>
		</div>
	</Sidebar.Footer>
</Sidebar.Root>
