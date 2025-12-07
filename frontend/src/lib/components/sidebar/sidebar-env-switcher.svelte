<script lang="ts">
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import * as Sidebar from '$lib/components/ui/sidebar/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { useSidebar } from '$lib/components/ui/sidebar/index.js';
	import ChevronsUpDownIcon from 'phosphor-svelte/lib/CaretUpDown';
	import PlusIcon from 'phosphor-svelte/lib/Plus';
	import RouterIcon from 'phosphor-svelte/lib/Broadcast';
	import ServerIcon from 'phosphor-svelte/lib/HardDrives';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import type { Environment } from '$lib/types/environment.type';
	import { goto } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import { m } from '$lib/paraglide/messages';
	import { cn } from '$lib/utils';
	import settingsStore from '$lib/stores/config-store';

	type Props = {
		isAdmin?: boolean;
	};

	let { isAdmin = false }: Props = $props();

	const sidebar = useSidebar();

	let dropdownOpen = $state(false);

	$effect(() => {
		if (sidebar.state === 'collapsed' && !sidebar.isHovered && dropdownOpen) {
			dropdownOpen = false;
		}
	});

	async function handleSelect(env: Environment) {
		if (!env || !env.enabled) return;
		try {
			await environmentStore.setEnvironment(env);
		} catch (error) {
			console.error('Failed to set environment:', error);
			toast.error('Failed to Connect to Environment');
		}
	}

	function getConnectionString(env: Environment): string {
		if (env.id === '0') {
			return $settingsStore.dockerHost || 'unix:///var/run/docker.sock';
		} else {
			return env.apiUrl;
		}
	}
</script>

<Sidebar.Menu>
	<Sidebar.MenuItem>
		<DropdownMenu.Root bind:open={dropdownOpen}>
			<DropdownMenu.Trigger>
				{#snippet child({ props: childProps })}
					<Sidebar.MenuButton
						{...childProps}
						size="lg"
						tooltipContent={environmentStore.selected ? environmentStore.selected.name : m.sidebar_no_environment()}
						class="data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
					>
						{#if environmentStore.selected}
							<div class="bg-primary text-primary-foreground flex aspect-square size-8 items-center justify-center rounded-lg">
								{#if environmentStore.selected.id === '0'}
									<ServerIcon class="size-4" />
								{:else}
									<RouterIcon class="size-4" />
								{/if}
							</div>
							<div class="grid flex-1 text-left text-sm leading-tight">
								<span class="truncate font-medium">
									{environmentStore.selected.name}
								</span>
								<span class="truncate text-xs">
									{getConnectionString(environmentStore.selected)}
								</span>
							</div>
						{:else}
							<div class="bg-primary text-primary-foreground flex aspect-square size-8 items-center justify-center rounded-lg">
								<ServerIcon class="size-4" />
							</div>
							<div class="grid flex-1 text-left text-sm leading-tight">
								<span class="truncate font-medium">{m.sidebar_no_environment()}</span>
								<span class="truncate text-xs">{m.sidebar_select_one()}</span>
							</div>
						{/if}
						<ChevronsUpDownIcon class="ml-auto" />
					</Sidebar.MenuButton>
				{/snippet}
			</DropdownMenu.Trigger>
			<DropdownMenu.Content
				class="w-(--bits-dropdown-menu-anchor-width) min-w-56 rounded-2xl border shadow-lg backdrop-blur-(--glass-blur-popup) backdrop-saturate-150"
				align="start"
				side="right"
				sideOffset={4}
			>
				<div
					role="group"
					onmouseenter={() => {
						// Keep sidebar hovered when mouse is over dropdown
						if (sidebar.state === 'collapsed') {
							sidebar.setHovered(true);
						}
					}}
					onmouseleave={() => {
						// Clear hover when leaving dropdown
						sidebar.setHovered(false, 200);
					}}
				>
					<DropdownMenu.Label class="text-muted-foreground text-xs">{m.sidebar_select_environment()}</DropdownMenu.Label>
					{#if environmentStore.available.length === 0}
						<DropdownMenu.Item disabled class="gap-2 p-2">
							<div class="flex size-6 items-center justify-center rounded-md border">
								<ServerIcon class="size-3.5 shrink-0" />
							</div>
							<span>{m.sidebar_no_environments()}</span>
						</DropdownMenu.Item>
					{:else}
						{#each environmentStore.available.slice(0, 5) as env (env.id)}
							{@const isActive = environmentStore.selected?.id === env.id}
							{@const isDisabled = !env.enabled}
							<DropdownMenu.Item
								onSelect={() => !isActive && !isDisabled && handleSelect(env)}
								disabled={isDisabled}
								class={cn(
									'gap-2 p-2',
									isActive && 'bg-sidebar-accent text-sidebar-accent-foreground pointer-events-none font-medium',
									isDisabled && 'cursor-not-allowed opacity-50'
								)}
							>
								<div
									class={cn(
										'flex size-6 items-center justify-center rounded-md border',
										isActive ? 'bg-primary border-primary' : 'border-border'
									)}
								>
									{#if env.id === '0'}
										<ServerIcon class={cn('size-3.5 shrink-0', isActive && 'text-primary-foreground')} />
									{:else}
										<RouterIcon class={cn('size-3.5 shrink-0', isActive && 'text-primary-foreground')} />
									{/if}
								</div>
								<div class="flex flex-col">
									<span>{env.name}</span>
									<span class={cn('text-xs', isActive ? 'text-sidebar-accent-foreground/70' : 'text-muted-foreground')}>
										{getConnectionString(env)}
									</span>
								</div>
							</DropdownMenu.Item>
						{/each}
						{#if environmentStore.available.length > 5}
							<DropdownMenu.Separator />
							<DropdownMenu.Item class="text-muted-foreground gap-2 p-2" onSelect={() => goto('/environments')}>
								<div class="flex size-6 items-center justify-center rounded-md border bg-transparent">
									<PlusIcon class="size-4" />
								</div>
								<div class="font-medium">View all {environmentStore.available.length} environments</div>
							</DropdownMenu.Item>
						{/if}
					{/if}
					{#if isAdmin}
						<DropdownMenu.Separator />
						<DropdownMenu.Item class="gap-2 p-2" onSelect={() => goto('/environments')}>
							<div class="flex size-6 items-center justify-center rounded-md border bg-transparent">
								<PlusIcon class="size-4" />
							</div>
							<div class="text-muted-foreground font-medium">{m.sidebar_manage_environments()}</div>
						</DropdownMenu.Item>
					{/if}
				</div>
			</DropdownMenu.Content>
		</DropdownMenu.Root>
	</Sidebar.MenuItem>
</Sidebar.Menu>
