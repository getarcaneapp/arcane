<script lang="ts">
	import { CopyButton } from '#lib/components/ui/copy-button/index.js';
	import { activityStore } from '#lib/stores/activity.store.svelte.js';
	import type { Activity } from '#lib/types/activity.type.js';
	import { ActivityIcon, TerminalIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import ActivityOutput from './activity-output.svelte';

	let { activity }: { activity: Activity } = $props();

	// Prefer the freshest activity data from the store (messages stream may update it).
	const liveActivity = $derived(activityStore.getActivity(activity.id) ?? activity);
	const detail = $derived(activityStore.getDetail(activity.id));
	const messages = $derived(detail?.messages ?? []);
	const outputText = $derived(messages.map((message) => message.message).join('\n'));
	const isLoading = $derived(activityStore.isDetailLoading(activity.id));
	const isDetailError = $derived(activityStore.isDetailError(activity.id));
</script>

<div class="border-b border-border/50 bg-muted/25">
	{#if liveActivity.error}
		<div class="border-b border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive">
			{liveActivity.error}
		</div>
	{/if}
	<div>
		<div class="flex items-center justify-between px-4 py-2.5">
			<div class="flex items-center gap-2">
				<TerminalIcon class="size-4 text-muted-foreground" aria-hidden="true" />
				<span class="text-sm font-semibold">{m.activity_output_title()}</span>
			</div>
			<CopyButton text={outputText} variant="ghost" size="default" class="h-8 px-2 text-xs" tabindex={0}>
				<span class="text-xs">{m.activity_copy_output()}</span>
			</CopyButton>
		</div>

		<div class="bg-zinc-950 font-mono text-[12px] leading-relaxed text-zinc-100">
			{#if isDetailError && messages.length === 0}
				<div class="flex min-h-32 flex-col items-center justify-center gap-2 text-zinc-500">
					<span>{m.activity_output_load_failed()}</span>
					<button
						type="button"
						onclick={() => activityStore.retryLoadDetail(activity.id)}
						class="text-xs text-primary underline hover:text-primary/80"
					>
						{m.common_retry()}
					</button>
				</div>
			{:else if isLoading && messages.length === 0}
				<div class="flex min-h-32 items-center justify-center text-zinc-500">
					<ActivityIcon class="mr-2 size-4 animate-pulse" aria-hidden="true" />
					{m.activity_output_loading()}
				</div>
			{:else if messages.length === 0}
				<div class="flex min-h-32 items-center justify-center text-center text-zinc-500">
					{m.activity_output_empty()}
				</div>
			{:else}
				<ActivityOutput {messages} />
			{/if}
		</div>
	</div>
</div>
