<script lang="ts">
	import { onMount } from 'svelte';
	import ResponsiveDialog from '$lib/components/ui/responsive-dialog/responsive-dialog.svelte';
	import ActivityCenterContent from './activity-center-content.svelte';
	import { activityStore } from '$lib/stores/activity.store.svelte';
	import { m } from '$lib/paraglide/messages';

	onMount(() => {
		void activityStore.start();
		return () => activityStore.stop();
	});

	function handleOpenChangeInternal(open: boolean) {
		activityStore.setOpen(open);
	}
</script>

<ResponsiveDialog
	open={activityStore.open}
	onOpenChange={handleOpenChangeInternal}
	variant="sheet"
	title={m.activity_center_title()}
	contentClass="w-[min(94vw,760px)] sm:max-w-[760px]"
	class="flex min-h-0 flex-1 flex-col pt-3 pb-0"
>
	<ActivityCenterContent />
</ResponsiveDialog>
