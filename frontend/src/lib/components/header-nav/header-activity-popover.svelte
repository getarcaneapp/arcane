<script lang="ts">
	import * as Popover from '$lib/components/ui/popover/index.js';
	import ActivityCenterContent from '$lib/components/activity/activity-center-content.svelte';
	import { activityStore } from '$lib/stores/activity.store.svelte';
	import { ActivityIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';

	let open = $state(false);

	const activeCount = $derived(activityStore.activeCount);
</script>

<Popover.Root bind:open>
	<Popover.Trigger
		title={m.activity_center_open()}
		aria-label={m.activity_center_open()}
		class="text-muted-foreground hover:bg-sidebar-accent/60 hover:text-foreground focus-visible:ring-ring data-[state=open]:bg-sidebar-accent/60 data-[state=open]:text-foreground relative flex size-9 shrink-0 items-center justify-center rounded-lg transition-colors focus-visible:ring-2 focus-visible:outline-hidden"
	>
		<ActivityIcon class="size-4.5" aria-hidden="true" />
		{#if activeCount > 0}
			<span
				aria-live="polite"
				aria-atomic="true"
				class="bg-primary text-primary-foreground absolute top-0.5 right-0.5 flex min-w-4 items-center justify-center rounded-full px-1 text-[10px] leading-4 font-bold tabular-nums"
			>
				{activeCount > 9 ? m.activity_count_many() : activeCount}
			</span>
		{/if}
	</Popover.Trigger>
	<Popover.Content align="end" sideOffset={10} class="flex w-[min(94vw,560px)] flex-col p-0">
		<div class="border-border/60 flex items-center justify-between border-b px-4 py-3">
			<h2 class="text-sm font-semibold">{m.activity_center_title()}</h2>
		</div>
		<ActivityCenterContent compact />
	</Popover.Content>
</Popover.Root>
