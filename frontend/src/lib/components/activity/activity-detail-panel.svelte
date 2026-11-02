<script lang="ts">
	import { CopyButton } from '#lib/components/ui/copy-button/index.js';
	import { ActivityIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { activityStore } from '#lib/stores/activity.store.svelte.js';
	import type { Activity } from '#lib/types/activity.type.js';
	import { cn } from '#lib/utils.js';

	import ActivityOutput from './activity-output.svelte';

	let {
		activity,
		fill = false
	}: {
		activity: Activity;
		/** Let the output grow into the remaining height instead of capping it. */
		fill?: boolean;
	} = $props();

	// Prefer the freshest activity data from the store (messages stream may update it).
	const liveActivity = $derived(activityStore.getActivity(activity.id) ?? activity);
	const detail = $derived(activityStore.getDetail(activity.id));
	const messages = $derived(detail?.messages ?? []);
	const outputText = $derived(messages.map((message) => message.message).join('\n'));
	const isLoading = $derived(activityStore.isDetailLoading(activity.id));
	const isDetailError = $derived(activityStore.isDetailError(activity.id));
</script>

<div class={cn('flex flex-col gap-3', fill && 'min-h-0 flex-1')}>
	{#if liveActivity.type === 'job_run' && liveActivity.latestMessage && liveActivity.latestMessage !== liveActivity.error}
		<p class="text-sm text-muted-foreground">{liveActivity.latestMessage}</p>
	{/if}
	{#if liveActivity.type !== 'job_run'}
		<section class={cn('flex flex-col gap-2', fill && 'min-h-0 flex-1')}>
			<div class="flex items-center justify-between gap-2">
				<h4 class="text-xs font-semibold text-muted-foreground">{m.activity_output_title()}</h4>
				{#if messages.length > 0}
					<CopyButton text={outputText} variant="ghost" size="sm" tabindex={0}>
						<span class="text-xs">{m.activity_copy_output()}</span>
					</CopyButton>
				{/if}
			</div>

			<div
				class={cn(
					'dark flex flex-col overflow-hidden rounded-lg border border-border/60 bg-background/80 font-mono text-xs leading-relaxed text-foreground',
					fill && 'min-h-56 flex-1'
				)}
			>
				{#if isDetailError && messages.length === 0}
					<div class="flex min-h-32 flex-1 flex-col items-center justify-center gap-2 text-muted-foreground">
						<span>{m.activity_output_load_failed()}</span>
						<button
							type="button"
							onclick={() => activityStore.retryLoadDetail(activity.id)}
							class="cursor-pointer text-xs text-primary hover:underline"
						>
							{m.common_retry()}
						</button>
					</div>
				{:else if isLoading && messages.length === 0}
					<div class="flex min-h-32 flex-1 items-center justify-center text-muted-foreground">
						<ActivityIcon class="mr-2 size-4 animate-pulse" aria-hidden="true" />
						{m.activity_output_loading()}
					</div>
				{:else if messages.length === 0}
					<div class="flex min-h-32 flex-1 items-center justify-center text-center text-muted-foreground">
						{m.activity_output_empty()}
					</div>
				{:else}
					<ActivityOutput {messages} class={cn(fill && 'max-h-none min-h-0 flex-1')} />
				{/if}
			</div>
		</section>
	{/if}
</div>
