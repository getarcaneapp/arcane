<script lang="ts">
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as Sidebar from '#lib/components/ui/sidebar/index.js';
	import { EnvironmentsIcon, RemoteEnvironmentIcon, EdgeConnectionIcon, ArrowsUpDownIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import { allEnvironmentsPreview, environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { PodmanStatus } from '#lib/types/podman.js';

	type Props = {
		onOpenDialog?: () => void;
		podmanStatus?: PodmanStatus | null;
	};

	let { onOpenDialog, podmanStatus = null }: Props = $props();

	function getConnectionString(): string {
		if (!environmentStore.selected) return '';
		if (environmentStore.selected.id === '0') {
			return settingsStore.current?.dockerHost || 'unix:///var/run/docker.sock';
		} else if (environmentStore.selected.isEdge) {
			return 'Edge connection';
		} else {
			return environmentStore.selected.apiUrl;
		}
	}
</script>

<!-- Hidden while the all environments preview shows every environment. -->
{#if !allEnvironmentsPreview.current}
	<Sidebar.Menu>
		<Sidebar.MenuItem>
			<Sidebar.MenuButton
				size="lg"
				tooltipContent={environmentStore.selected ? environmentStore.selected.name : m.sidebar_no_environment()}
				onclick={() => onOpenDialog?.()}
			>
				{#if environmentStore.selected}
					<div class="flex aspect-square size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
						{#if environmentStore.selected.id === '0'}
							<EnvironmentsIcon class="size-4" />
						{:else if environmentStore.selected.isEdge}
							<EdgeConnectionIcon class="size-4" />
						{:else}
							<RemoteEnvironmentIcon class="size-4" />
						{/if}
					</div>
					<div class="grid flex-1 text-left text-sm leading-tight">
						<span class="flex min-w-0 items-center gap-1.5">
							<span class="truncate font-medium">
								{environmentStore.selected.name}
							</span>
							{#if podmanStatus?.detected}
								<Badge variant="purple" size="xs">{m.podman()}</Badge>
							{/if}
						</span>
						<span class="truncate text-xs">
							{getConnectionString()}
						</span>
					</div>
				{:else}
					<div class="flex aspect-square size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
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
{/if}
