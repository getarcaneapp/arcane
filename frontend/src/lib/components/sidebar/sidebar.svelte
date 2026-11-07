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
	import type { PodmanStatus } from '#lib/types/podman.js';
	import type { PermissionsManifest } from '#lib/types/role.js';
	import type { User } from '#lib/types/user.js';
	import type { VersionInfo } from '#lib/types/version.js';

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
		versionInformation: VersionInfo;
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

	const isCollapsed = $derived(sidebar.state === 'collapsed');
	const overviewItems = $derived(filterByPermissions(navigationItems.overviewItems, effectiveUser ?? null, permissionsManifest));
	const deployItems = $derived(filterByPermissions(navigationItems.deployItems, effectiveUser ?? null, permissionsManifest));
	const resourceItems = $derived(filterByPermissions(navigationItems.resourceItems, effectiveUser ?? null, permissionsManifest));
	const swarmItems = $derived(
		filterByPermissions(getSwarmNavigationItems(swarmEnabled), effectiveUser ?? null, permissionsManifest)
	);
	const podmanItems = $derived(
		filterByPermissions(getPodmanNavigationItems(podmanStatuses), effectiveUser ?? null, permissionsManifest)
	);
	const adminItems = $derived(filterByPermissions(navigationItems.adminItems, effectiveUser ?? null, permissionsManifest));
</script>

<VersionInfoDialog
	bind:open={showVersionDialog}
	onOpenChange={(open) => (showVersionDialog = open)}
	versionInfo={versionInformation}
	debugMode={false}
/>

<Sidebar.Root {collapsible} {variant} {...restProps}>
	<Sidebar.Header collapsed={isCollapsed}>
		<SidebarLogo {isCollapsed} />
	</Sidebar.Header>
	<Sidebar.Content>
		{#if overviewItems.length > 0}
			<SidebarItemGroup label={m.sidebar_overview()} items={overviewItems} />
		{/if}
		{#if deployItems.length > 0}
			<SidebarItemGroup label={m.deploy()} items={deployItems} />
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
		{#if adminItems.length > 0}
			<SidebarItemGroup label={m.sidebar_administration()} items={adminItems} />
		{/if}
	</Sidebar.Content>
	<Sidebar.Footer collapsed={isCollapsed}>
		<SidebarUpdatebanner {isCollapsed} {versionInformation} debug={false} />
		{#if isCollapsed}
			<div class="flex flex-col items-center gap-1">
				<ActivityCenterTrigger collapsed compact />
				{#if effectiveUser}
					<SidebarUser {isCollapsed} user={effectiveUser} {autoLoginEnabled} />
				{/if}
				<SidebarPinButton class="size-9.5" />
			</div>
		{:else}
			<div class="flex items-center gap-1">
				<button
					type="button"
					onclick={() => (showVersionDialog = true)}
					class="min-w-0 flex-1 cursor-pointer truncate pl-2.5 text-left text-xs font-medium text-muted-foreground tabular-nums transition-colors hover:text-foreground"
				>
					{m.sidebar_version({
						version: versionInformation?.displayVersion ?? versionInformation?.currentVersion ?? m.common_unknown()
					})}
				</button>
				<ActivityCenterTrigger collapsed compact class="mx-0" />
			</div>
			<div class="flex items-center gap-1">
				{#if effectiveUser}
					<div class="min-w-0 flex-1">
						<SidebarUser {isCollapsed} user={effectiveUser} {autoLoginEnabled} />
					</div>
				{/if}
				<SidebarPinButton class="ml-auto size-9.5 shrink-0" />
			</div>
		{/if}
	</Sidebar.Footer>
</Sidebar.Root>
