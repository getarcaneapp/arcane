<script lang="ts">
	import * as Sidebar from '$lib/components/ui/sidebar/index.js';
	import ChevronsUpDownIcon from '@lucide/svelte/icons/chevrons-up-down';
	import RouterIcon from '@lucide/svelte/icons/router';
	import ServerIcon from '@lucide/svelte/icons/server';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { m } from '$lib/paraglide/messages';
	import settingsStore from '$lib/stores/config-store';
	import { EnvironmentSelector } from '$lib/components/environment-selector/index.js';

	type Props = {
		isAdmin?: boolean;
	};

	let { isAdmin = false }: Props = $props();

	let selectorOpen = $state(false);

	function getConnectionString(apiUrl: string, id: string): string {
		if (id === '0') {
			return $settingsStore.dockerHost || 'unix:///var/run/docker.sock';
		}
		return apiUrl;
	}
</script>

<Sidebar.Menu>
	<Sidebar.MenuItem>
		<EnvironmentSelector bind:open={selectorOpen} {isAdmin}>
			{#snippet trigger()}
				<Sidebar.MenuButton
					size="lg"
					tooltipContent={environmentStore.selected ? environmentStore.selected.name : m.sidebar_no_environment()}
					class="data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
					onclick={() => (selectorOpen = true)}
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
								{getConnectionString(environmentStore.selected.apiUrl, environmentStore.selected.id)}
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
		</EnvironmentSelector>
	</Sidebar.MenuItem>
</Sidebar.Menu>
