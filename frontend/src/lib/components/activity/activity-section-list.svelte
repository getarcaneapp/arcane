<script lang="ts">
	import { activityStore } from '#lib/stores/activity.store.svelte.js';

	import { activityGroupKey, buildActivitySections } from './activity-groups';
	import ActivityRow from './activity-row.svelte';

	let {
		selectedKey = null,
		onSelect
	}: {
		selectedKey?: string | null;
		onSelect: (key: string) => void;
	} = $props();

	const sections = $derived(buildActivitySections(activityStore.runningGroups, activityStore.historyGroups));
</script>

<div class="flex flex-col px-2 pb-2">
	{#each sections as section (section.id)}
		<div class="flex items-baseline justify-between px-2.5 pt-3 pb-1 text-2xs font-medium text-muted-foreground">
			<h3>{section.label}</h3>
			<span class="tabular-nums">{section.groups.length}</span>
		</div>
		{#each section.groups as group (activityGroupKey(group))}
			{@const key = activityGroupKey(group)}
			<ActivityRow {group} selected={key === selectedKey} time="clock" {onSelect} />
		{/each}
	{/each}
</div>
