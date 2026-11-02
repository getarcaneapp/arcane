<script lang="ts">
	import type { Snippet } from 'svelte';

	import { m } from '#lib/paraglide/messages.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { Environment } from '#lib/types/environment.js';
	import { DASHBOARD_WIDGET_TYPES, type DashboardWidget } from '#lib/utils/dashboard-layout.js';

	import DashboardPanel from '../dashboard-panel.svelte';

	let { widget, environmentCard }: { widget: DashboardWidget; environmentCard: Snippet<[Environment]> } = $props();

	const environmentId = $derived(widget.config?.environmentIds?.[0]);
	const environment = $derived(environmentStore.available.find((entry) => entry.id === environmentId && entry.enabled));
</script>

{#if environment}
	<div class="h-full min-h-0 overflow-y-auto">
		{@render environmentCard(environment)}
	</div>
{:else}
	<DashboardPanel title={widget.title || DASHBOARD_WIDGET_TYPES.environment.label()}>
		<p class="py-2 text-sm text-muted-foreground">
			{environmentId ? m.common_not_found_title({ resource: m.resource_environment_cap() }) : m.common_not_configured()}
		</p>
	</DashboardPanel>
{/if}
