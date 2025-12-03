<script lang="ts" module>
	import { navigationItems } from '$lib/config/navigation-config';
</script>

<script lang="ts">
	import SidebarItemGroup from './sidebar-itemgroup.svelte';
	import SidebarUser from './sidebar-user.svelte';
	import SidebarEnvSwitcher from './sidebar-env-switcher.svelte';
	import * as Sidebar from '$lib/components/ui/sidebar/index.js';
	import { useSidebar } from '$lib/components/ui/sidebar/index.js';
	import type { ComponentProps } from 'svelte';
	import type { User } from '$lib/types/user.type';
	import type { AppVersionInformation } from '$lib/types/application-configuration';
	import SidebarLogo from './sidebar-logo.svelte';
	import SidebarUpdatebanner from './sidebar-updatebanner.svelte';
	import SidebarPinButton from './sidebar-pin-button.svelte';
	import userStore from '$lib/stores/user-store';
	import { m } from '$lib/paraglide/messages';
	import * as Button from '$lib/components/ui/button/index.js';
	import LogOutIcon from '@lucide/svelte/icons/log-out';

	let {
		ref = $bindable(null),
		collapsible = 'icon',
		variant = 'floating',
		user,
		versionInformation,
		...restProps
	}: ComponentProps<typeof Sidebar.Root> & {
		versionInformation: AppVersionInformation;
		user?: User | null;
	} = $props();

	const sidebar = useSidebar();

	let storeUser: User | null = $state(null);
	$effect(() => {
		const unsub = userStore.subscribe((u) => (storeUser = u));
		return unsub;
	});
	const effectiveUser = $derived(user ?? storeUser);

	const isCollapsed = $derived(sidebar.state === 'collapsed' && !(sidebar.hoverExpansionEnabled && sidebar.isHovered));
	const isAdmin = $derived(!!effectiveUser?.roles?.includes('admin'));
</script>

<Sidebar.Root {collapsible} {variant} {...restProps}>
	<Sidebar.Header class={isCollapsed ? 'gap-0 p-1 pb-2' : ''}>
		{#if isCollapsed}
			<div class="flex justify-center">
				<SidebarPinButton />
			</div>
		{/if}
		<div class="relative">
			<SidebarLogo {isCollapsed} {versionInformation} />
			{#if !isCollapsed}
				<div class="absolute top-0 right-0 -mt-1 -mr-1">
					<SidebarPinButton />
				</div>
			{/if}
		</div>
		{#if isCollapsed}
			<div class="flex justify-center px-1">
				<SidebarEnvSwitcher {isAdmin} />
			</div>
		{:else}
			<SidebarEnvSwitcher {isAdmin} />
		{/if}
	</Sidebar.Header>
	<Sidebar.Content class={!isCollapsed ? '-mt-2' : ''}>
		<SidebarItemGroup label={m.sidebar_management()} items={navigationItems.managementItems} />
		<SidebarItemGroup label={m.sidebar_customization()} items={navigationItems.customizationItems} />
		{#if isAdmin}
			<SidebarItemGroup label={m.sidebar_environments()} items={navigationItems.environmentItems} />
			<SidebarItemGroup label={m.sidebar_administration()} items={navigationItems.settingsItems} />
		{/if}
	</Sidebar.Content>
	<Sidebar.Footer>
		<SidebarUpdatebanner
			{isCollapsed}
			{versionInformation}
			updateAvailable={versionInformation.updateAvailable}
			user={effectiveUser}
			debug={false}
		/>
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
						<form action="/logout" method="POST" class="ml-auto">
							<Button.Root
								variant="ghost"
								title={m.common_logout()}
								type="submit"
								class="text-muted-foreground hover:text-destructive hover:bg-destructive/10 h-9 w-9 rounded-xl p-0"
							>
								<LogOutIcon size={16} />
							</Button.Root>
						</form>
					</div>
				</div>
			{/if}
		{/if}
		<div class={`flex items-center justify-center ${isCollapsed ? 'px-1' : 'px-4'}`}>
			<span class="text-muted-foreground/60 text-xs font-medium">
				{m.sidebar_version({
					version: versionInformation?.displayVersion ?? versionInformation?.currentVersion ?? m.common_unknown()
				})}
			</span>
		</div>
	</Sidebar.Footer>
</Sidebar.Root>
