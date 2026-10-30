<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';

	import EnvironmentDestinationSelect from '#lib/components/form/environment-destination-select.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';

	let {
		selectedId,
		environments,
		offlineCount
	}: {
		selectedId: string;
		environments: EnvironmentRef[];
		offlineCount: number;
	} = $props();

	function select(environmentId: string | undefined) {
		const url = new URL(page.url.href);
		if (environmentId) url.searchParams.set('env', environmentId);
		else url.searchParams.delete('env');
		void goto(url, { reset: false });
	}
</script>

<div class="flex flex-wrap items-center gap-x-3 gap-y-2">
	<span class="text-sm text-muted-foreground">{m.applying_settings_for()}</span>
	<EnvironmentDestinationSelect bind:value={() => selectedId, select} {environments} allOption size="sm" />
	{#if offlineCount > 0}
		<span class="text-xs text-warning">{m.offline_environments_not_included({ count: offlineCount })}</span>
	{/if}
</div>
