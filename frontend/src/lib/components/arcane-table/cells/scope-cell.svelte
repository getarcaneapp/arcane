<script lang="ts" module>
	import { m } from '#lib/paraglide/messages.js';

	export function scopeLabel(allEnvironments: boolean, environmentIds: string[], envNameById: Map<string, string>): string {
		if (allEnvironments || environmentIds.length === 0) return m.all_environments();
		return environmentIds.map((id) => envNameById.get(id) ?? id).join(', ');
	}
</script>

<script lang="ts">
	import { Badge } from '#lib/components/ui/badge/index.js';

	let {
		allEnvironments,
		environmentIds,
		envNameById
	}: {
		allEnvironments: boolean;
		environmentIds: string[];
		envNameById: Map<string, string>;
	} = $props();
</script>

{#if allEnvironments || environmentIds.length === 0}
	<Badge variant="outline" size="sm">{m.all_environments()}</Badge>
{:else}
	<div class="flex flex-wrap items-center gap-1">
		{#each environmentIds.slice(0, 2) as environmentId (environmentId)}
			<Badge variant="outline" size="sm" class="max-w-40"
				><span class="block min-w-0 truncate">{envNameById.get(environmentId) ?? environmentId}</span></Badge
			>
		{/each}
		{#if environmentIds.length > 2}
			<Badge variant="gray" size="sm">{m.plus_count({ count: environmentIds.length - 2 })}</Badge>
		{/if}
	</div>
{/if}
