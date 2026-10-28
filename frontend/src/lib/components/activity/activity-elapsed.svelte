<script lang="ts">
	import { onMount } from 'svelte';

	import type { Activity } from '#lib/types/activity.type.js';

	import { activityDurationLabel } from './activity-labels';

	let { activity }: { activity: Activity } = $props();

	// Re-render once a second; only mounted for running work, so the cost is bounded.
	let tick = $state(0);
	onMount(() => {
		const interval = setInterval(() => (tick += 1), 1000);
		return () => clearInterval(interval);
	});

	const label = $derived.by(() => {
		void tick;
		return activityDurationLabel(activity);
	});
</script>

<span class="tabular-nums">{label}</span>
