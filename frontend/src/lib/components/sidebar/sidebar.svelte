<script lang="ts" module>
	import {
		navigationItems,
		getSwarmNavigationItems,
		getPodmanNavigationItems,
		filterByPermissions
	} from '#lib/config/navigation-config.js';
</script>

<script lang="ts">
	import type { ComponentProps } from 'svelte';

	import ActivityCenterTrigger from '#lib/components/activity/activity-center-trigger.svelte';
	import VersionInfoDialog from '#lib/components/dialogs/version-info-dialog.svelte';
	import * as Sidebar from '#lib/components/ui/sidebar/index.js';
	import { useSidebar } from '#lib/components/ui/sidebar/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import type { PermissionsManifest, User } from '#lib/types/auth.js';
	import type { PodmanStatus } from '#lib/types/podman.js';
	import type { AppVersionInformation } from '#lib/types/settings.js';

	import SidebarItemGroup from './sidebar-itemgroup.svelte';
	import SidebarLogo from './sidebar-logo.svelte';
	import SidebarPinButton from './sidebar-pin-button.svelte';
	import SidebarUpdatebanner from './sidebar-updatebanner.svelte';
	import SidebarUser from './sidebar-user.svelte';

	let {
		ref = $bindable(null),
		collapsible = 'icon',
		variant = 'floating',
		user,
		versionInformation,
		swarmEnabled,
		podmanStatuses = {},
		permissionsManifest = null,
		...restProps
	}: ComponentProps<typeof Sidebar.Root> & {
		versionInformation: AppVersionInformation;
		user?: User | null;
		swarmEnabled?: boolean;
		podmanStatuses?: Record<string, PodmanStatus>;
		permissionsManifest?: PermissionsManifest | null;
	} = $props();

	const autoLogin = settingsStore.autoLoginEnabled;
	const autoLoginEnabled = $derived(autoLogin.current);

	const sidebar = useSidebar();

	const storeUser = userStore;
	let showVersionDialog = $state(false);
	const effectiveUser = $derived(storeUser.current ?? user);

	const isCollapsed = $derived(sidebar.state === 'collapsed' && !(sidebar.hoverExpansionEnabled && sidebar.isHovered));
	const managementItems = $derived(
		filterByPermissions(navigationItems.managementItems, effectiveUser ?? null, permissionsManifest)
	);
	const resourceItems = $derived(filterByPermissions(navigationItems.resourceItems, effectiveUser ?? null, permissionsManifest));
	const swarmItems = $derived(
		filterByPermissions(getSwarmNavigationItems(swarmEnabled), effectiveUser ?? null, permissionsManifest)
	);
	const podmanItems = $derived(
		filterByPermissions(getPodmanNavigationItems(podmanStatuses), effectiveUser ?? null, permissionsManifest)
	);
	const settingsItems = $derived(filterByPermissions(navigationItems.settingsItems, effectiveUser ?? null, permissionsManifest));
</script>

<VersionInfoDialog
	bind:open={showVersionDialog}
	onOpenChange={(open) => (showVersionDialog = open)}
	versionInfo={versionInformation}
	debugMode={false}
/>

<Sidebar.Root {collapsible} {variant} {...restProps}>
	<Sidebar.Header collapsed={isCollapsed}>
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
			<div class="flex justify-center px-1 pt-1">
				<ActivityCenterTrigger collapsed compact />
			</div>
		{:else}
			<div class="pt-1">
				<Sidebar.Menu>
					<Sidebar.MenuItem>
						<ActivityCenterTrigger compact />
					</Sidebar.MenuItem>
				</Sidebar.Menu>
			</div>
		{/if}
	</Sidebar.Header>
	<Sidebar.Content class={!isCollapsed ? '-mt-2' : ''}>
		{#if managementItems.length > 0}
			<SidebarItemGroup label={m.sidebar_management()} items={managementItems} />
		{/if}
		{#if resourceItems.length > 0}
			<SidebarItemGroup label={m.resources()} items={resourceItems} />
		{/if}
		{#if swarmItems.length > 0}
			<SidebarItemGroup label={m.swarm()} items={swarmItems} />
		{/if}
		{#if podmanItems.length > 0}
			<SidebarItemGroup label={m.podman()} items={podmanItems} />
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
				class="cursor-pointer text-xs font-medium text-muted-foreground/60 transition-colors hover:text-muted-foreground"
			>
				{m.sidebar_version({
					version: versionInformation?.displayVersion ?? versionInformation?.currentVersion ?? m.common_unknown()
				})}
			</button>
		</div>
	</Sidebar.Footer>
</Sidebar.Root>
