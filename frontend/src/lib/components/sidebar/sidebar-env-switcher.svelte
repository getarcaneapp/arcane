<script lang="ts">
	import * as Sidebar from '$lib/components/ui/sidebar/index.js';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { m } from '$lib/paraglide/messages';
	import settingsStore from '$lib/stores/config-store';
	import { EnvironmentsIcon, RemoteEnvironmentIcon, EdgeConnectionIcon, ArrowsUpDownIcon } from '$lib/icons';

	type Props = {
		onOpenDialog?: () => void;
	};

	let { onOpenDialog }: Props = $props();

	function getConnectionString(): string {
		if (!environmentStore.selected) return '';
		if (environmentStore.selected.id === '0') {
			return $settingsStore.dockerHost || 'unix:///var/run/docker.sock';
		} else if (environmentStore.selected.isEdge) {
			return 'Edge connection';
		} else {
			return environmentStore.selected.apiUrl;
		}
	}
</script>

<Sidebar.Menu>
	<Sidebar.MenuItem>
		<Sidebar.MenuButton
			size="lg"
			tooltipContent={environmentStore.selected ? environmentStore.selected.name : m.sidebar_no_environment()}
			class="data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
			onclick={() => onOpenDialog?.()}
		>
			{#if environmentStore.selected}
				<div class="bg-primary text-primary-foreground flex aspect-square size-8 items-center justify-center rounded-lg">
					{#if environmentStore.selected.id === '0'}
						<EnvironmentsIcon class="size-4" />
					{:else if environmentStore.selected.isEdge}
						<EdgeConnectionIcon class="size-4" />
					{:else}
						<RemoteEnvironmentIcon class="size-4" />
					{/if}
				</div>
				<div class="grid flex-1 text-left text-sm leading-tight">
					<span class="truncate font-medium">
						{environmentStore.selected.name}
					</span>
					<span class="truncate text-xs">
						{getConnectionString()}
					</span>
				</div>
			{:else}
				<div class="bg-primary text-primary-foreground flex aspect-square size-8 items-center justify-center rounded-lg">
					<EnvironmentsIcon class="size-4" />
				</div>
				<div class="grid flex-1 text-left text-sm leading-tight">
					<span class="truncate font-medium">{m.sidebar_no_environment()}</span>
					<span class="truncate text-xs">{m.sidebar_select_one()}</span>
				</div>
			{/if}
			<ArrowsUpDownIcon class="ml-auto" />
		</Sidebar.MenuButton>
	</Sidebar.MenuItem>
</Sidebar.Menu>
