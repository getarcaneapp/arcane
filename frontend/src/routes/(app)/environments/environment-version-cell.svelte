<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import systemUpgradeService from '#lib/services/api/system-upgrade-service.js';
	import type { Environment } from '#lib/types/environment.js';
	import { UpdateIcon } from '#lib/icons/index.js';
	import { createQuery } from '@tanstack/svelte-query';

	let { environment, isOnline }: { environment: Environment; isOnline: boolean } = $props();

	// Shares its key with the upgrade menu item, so opening a row's actions reuses this
	// result instead of asking the agent again. Offline environments are never queried:
	// remote version lookups are proxied through the agent and would just time out.
	const versionQuery = createQuery(() => ({
		queryKey: queryKeys.system.versionInfo(environment.id),
		queryFn: () => systemUpgradeService.getVersionInfo(environment.id),
		enabled: isOnline,
		retry: false
	}));

	const info = $derived(versionQuery.data ?? null);
	const label = $derived(info?.displayVersion || info?.currentVersion || info?.currentTag || '');
</script>

{#if label}
	<div class="flex items-center gap-1.5">
		<span class="font-mono text-sm">{label}</span>
		{#if info?.updateAvailable}
			<span
				class="inline-flex items-center text-amber-600 dark:text-amber-400"
				title={info.newestVersion
					? m.sidebar_update_available_tooltip({ version: info.newestVersion })
					: m.sidebar_update_available()}
			>
				<UpdateIcon class="size-3.5" />
			</span>
		{/if}
	</div>
{:else}
	<span class="text-sm text-muted-foreground">—</span>
{/if}
