<script lang="ts" module>
	import { navigationItems, getManagementItems } from '$lib/config/navigation-config';
</script>

<script lang="ts">
	import SidebarItemGroup from './sidebar-itemgroup.svelte';
	import SidebarUser from './sidebar-user.svelte';
	import SidebarEnvSwitcher from './sidebar-env-switcher.svelte';
	import EnvironmentSwitcherDialog from '$lib/components/dialogs/environment-switcher-dialog.svelte';
	import * as Sidebar from '$lib/components/ui/sidebar/index.js';
	import { useSidebar } from '$lib/components/ui/sidebar/index.js';
	import type { ComponentProps } from 'svelte';
	import type { User } from '$lib/types/user.type';
	import type { AppVersionInformation } from '$lib/types/application-configuration';
	import type { DockerInfo } from '$lib/types/docker-info.type';
	import SidebarLogo from './sidebar-logo.svelte';
	import SidebarUpdatebanner from './sidebar-updatebanner.svelte';
	import SidebarPinButton from './sidebar-pin-button.svelte';
	import userStore from '$lib/stores/user-store';
	import settingsStore from '$lib/stores/config-store';
	import { m } from '$lib/paraglide/messages';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import VersionInfoDialog from '$lib/components/dialogs/version-info-dialog.svelte';
	import { LogoutIcon } from '$lib/icons';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { fromStore } from 'svelte/store';

	let {
		ref = $bindable(null),
		collapsible = 'icon',
		variant = 'floating',
		user,
		versionInformation,
		dockerInfo = null,
		...restProps
	}: ComponentProps<typeof Sidebar.Root> & {
		versionInformation: AppVersionInformation;
		user?: User | null;
		dockerInfo?: DockerInfo | null;
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
	const isAdmin = $derived(!!effectiveUser?.roles?.includes('admin'));
	let envSwitcherOpen = $state(false);

	// Filter out sub-items for settings on desktop since we have a dedicated settings sidebar
	const desktopSettingsItems =
		navigationItems.settingsItems?.map((item) => {
			if (item.url === '/settings') {
				const { items, ...rest } = item;
				return rest;
			}
			return item;
		}) ?? [];

	const currentEnvId = $derived(environmentStore.selected?.id || '0');
	const managementItems = $derived(getManagementItems(currentEnvId));
	const swarmEnabled = $derived(dockerInfo?.Swarm?.LocalNodeState === 'active');
</script>

<VersionInfoDialog
	bind:open={showVersionDialog}
	onOpenChange={(open) => (showVersionDialog = open)}
	versionInfo={versionInformation}
/>

<EnvironmentSwitcherDialog bind:open={envSwitcherOpen} {isAdmin} />

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
	</Sidebar.Header>
	<Sidebar.Content class={!isCollapsed ? '-mt-2' : ''}>
		<SidebarItemGroup label={m.sidebar_management()} items={managementItems} />
		<SidebarItemGroup label={m.sidebar_resources()} items={navigationItems.resourceItems} />
		{#if swarmEnabled}
			<SidebarItemGroup label={m.swarm_title()} items={navigationItems.swarmItems} />
		{/if}
		<SidebarItemGroup label={m.security_title()} items={navigationItems.securityItems} />
		{#if isAdmin}
			<SidebarItemGroup label={m.sidebar_administration()} items={desktopSettingsItems} />
		{/if}
	</Sidebar.Content>
	<Sidebar.Footer>
		<SidebarUpdatebanner {isCollapsed} {versionInformation} user={effectiveUser} debug={false} />
		{#if effectiveUser}
			{#if isCollapsed}
				<div class="px-0 pb-2">
					<div class="flex flex-col items-center gap-2">
						<SidebarUser {isCollapsed} user={effectiveUser} />
					</div>
				</div>
			{:else}
				<div class="px-3 pb-2">
					<div class="flex items-center gap-2">
						<SidebarUser {isCollapsed} user={effectiveUser} />
						{#if !autoLoginEnabled}
							<form action="/logout" method="POST" class="ml-auto">
								<ArcaneButton
									action="base"
									tone="ghost"
									title={m.common_logout()}
									type="submit"
									class="text-muted-foreground hover:text-destructive hover:bg-destructive/10 h-9 w-9 rounded-xl p-0"
									icon={LogoutIcon}
									showLabel={false}
									customLabel={m.common_logout()}
								/>
							</form>
						{/if}
					</div>
				</div>
			{/if}
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
