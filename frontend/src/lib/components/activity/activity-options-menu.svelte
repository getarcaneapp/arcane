<script lang="ts">
	import { toast } from 'svelte-sonner';

	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { EllipsisIcon, TrashIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { activityStore } from '#lib/stores/activity.store.svelte.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import { activityCompletionToastsEnabled } from './activity-completion-toasts';
	import ActivityIconButton from './activity-icon-button.svelte';
	import { activityCenterLayout, activityCenterLayouts, isActivityCenterLayout } from './activity-layout';

	function clearHistoryInternal() {
		openConfirmDialog({
			title: m.activity_clear_history_title(),
			message: m.activity_clear_history_message(),
			confirm: {
				label: m.activity_clear_history_confirm(),
				destructive: true,
				action: async () => {
					const result = await tryCatch(activityStore.clearHistory());
					if (result.error !== null) {
						console.error('Failed to clear activity history:', result.error);
						toast.error(m.activity_clear_history_failed());
						return;
					}
					if (result.data.failed.length > 0) {
						toast.warning(
							m.activity_clear_history_partial({
								count: result.data.succeeded,
								environments: result.data.failed.map((failure) => failure.environmentName).join(', ')
							})
						);
						return;
					}
					toast.success(m.activity_clear_history_success());
				}
			}
		});
	}
</script>

<DropdownMenu.Root>
	<DropdownMenu.Trigger title={m.activity_more_options()} aria-label={m.activity_more_options()}>
		{#snippet child({ props })}
			<ActivityIconButton {...props} label={m.activity_more_options()}>
				<EllipsisIcon class="size-4" aria-hidden="true" />
			</ActivityIconButton>
		{/snippet}
	</DropdownMenu.Trigger>
	<DropdownMenu.Content align="end" class="w-56">
		<DropdownMenu.Group>
			<DropdownMenu.Label>{m.activity_layout_label()}</DropdownMenu.Label>
			<DropdownMenu.RadioGroup
				value={activityCenterLayout.current}
				onValueChange={(value) => {
					if (isActivityCenterLayout(value)) activityCenterLayout.current = value;
				}}
			>
				{#each activityCenterLayouts as layout (layout.value)}
					<DropdownMenu.RadioItem value={layout.value}>{layout.label()}</DropdownMenu.RadioItem>
				{/each}
			</DropdownMenu.RadioGroup>
		</DropdownMenu.Group>
		<DropdownMenu.Separator />
		<DropdownMenu.CheckboxItem
			checked={activityCompletionToastsEnabled.current}
			onCheckedChange={(checked) => (activityCompletionToastsEnabled.current = checked)}
			closeOnSelect={false}
		>
			{m.activity_completion_toasts_label()}
		</DropdownMenu.CheckboxItem>
		<IfPermitted perm="activities:delete">
			<DropdownMenu.Separator />
			<DropdownMenu.Item onclick={clearHistoryInternal} variant="destructive">
				<TrashIcon class="size-4" aria-hidden="true" />
				{m.activity_clear_history()}
			</DropdownMenu.Item>
		</IfPermitted>
	</DropdownMenu.Content>
</DropdownMenu.Root>
