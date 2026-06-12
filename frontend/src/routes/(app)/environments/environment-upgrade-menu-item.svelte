<script lang="ts">
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { UpdateIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { queryKeys } from '$lib/query/query-keys';
	import systemUpgradeService from '$lib/services/api/system-upgrade-service';
	import type { AppVersionInformation } from '$lib/types/settings';
	import type { Environment } from '$lib/types/environment';
	import { createQuery } from '@tanstack/svelte-query';

	let {
		environment,
		isOnline,
		disabled = false,
		isUpgradingThis = false,
		onSelect
	}: {
		environment: Environment;
		isOnline: boolean;
		disabled?: boolean;
		isUpgradingThis?: boolean;
		onSelect: (env: Environment, versionInfo: AppVersionInformation) => void;
	} = $props();

	const versionQuery = createQuery(() => ({
		queryKey: queryKeys.system.versionInfo(environment.id),
		queryFn: () => systemUpgradeService.getVersionInfo(environment.id),
		enabled: isOnline && environment.id !== '0',
		staleTime: 60 * 1000,
		retry: false
	}));

	const versionInfo = $derived(versionQuery.data ?? null);
	const updateAvailable = $derived(!!versionInfo?.updateAvailable);

	const itemLabel = $derived.by(() => {
		if (isUpgradingThis) return m.upgrade_in_progress();
		if (versionInfo?.newestVersion) return m.upgrade_to_version({ version: versionInfo.newestVersion });
		if (versionInfo?.currentTag) return m.upgrade_update_tag({ tag: versionInfo.currentTag });
		return m.upgrade_now();
	});
</script>

{#if updateAvailable && versionInfo}
	<DropdownMenu.Item onclick={() => onSelect(environment, versionInfo)} {disabled}>
		<UpdateIcon class="size-4" />
		{itemLabel}
	</DropdownMenu.Item>
{/if}
