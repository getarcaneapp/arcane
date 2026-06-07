<script lang="ts">
	import { Progress } from '$lib/components/ui/progress/index.js';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { CopyButton } from '$lib/components/ui/copy-button';
	import { activityStore } from '$lib/stores/activity.store.svelte';
	import type { Activity, ActivityMessage } from '$lib/types/activity.type';
	import { ActivityIcon, CloseIcon, TerminalIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { cn } from '$lib/utils';
	import IfPermitted from '$lib/components/if-permitted.svelte';
	import { confirmCancelActivity } from './activity-cancel';
	import { activityStatusLabel, activityStatusVariant, activityTypeIcon, activityTypeLabel } from './activity-labels';

	let { activity }: { activity: Activity } = $props();

	let outputContainer = $state<HTMLElement | null>(null);

	// Prefer the freshest activity data from the store (messages stream may update it).
	const liveActivity = $derived(activityStore.getActivity(activity.id) ?? activity);
	const detail = $derived(activityStore.getDetail(activity.id));
	const messages = $derived(detail?.messages ?? []);
	const outputText = $derived(messages.map(formatOutputLineInternal).join('\n'));
	const IconComponent = $derived(activityTypeIcon(liveActivity.type));
	const hasProgress = $derived(typeof liveActivity.progress === 'number');
	const progressValue = $derived(Math.min(100, Math.max(0, Math.round(liveActivity.progress ?? 0))));
	const isLoading = $derived(activityStore.isDetailLoading(activity.id));
	const isDetailError = $derived(activityStore.isDetailError(activity.id));
	const activityTarget = $derived(
		liveActivity.resourceName || liveActivity.resourceId || liveActivity.resourceType || m.activity_unknown_target()
	);
	const sourceEnvironmentName = $derived(
		liveActivity.sourceEnvironmentName || liveActivity.sourceEnvironmentId || liveActivity.environmentId
	);
	const startedByName = $derived(liveActivity.startedBy?.displayName || liveActivity.startedBy?.username);
	const cancelable = $derived(liveActivity.status === 'running' || liveActivity.status === 'queued');

	$effect(() => {
		messages.length;
		outputContainer;
		queueMicrotask(() => {
			if (!outputContainer) {
				return;
			}
			outputContainer.scrollTop = outputContainer.scrollHeight;
		});
	});

	function formatDateTimeInternal(value?: string): string {
		if (!value) {
			return m.common_na();
		}
		return new Intl.DateTimeFormat(undefined, {
			month: 'short',
			day: 'numeric',
			hour: 'numeric',
			minute: '2-digit',
			second: '2-digit'
		}).format(new Date(value));
	}

	function formatDurationInternal(value: Activity | null): string {
		const durationMs = value?.durationMs ?? (value?.startedAt ? Date.now() - new Date(value.startedAt).getTime() : 0);
		if (!durationMs || Number.isNaN(durationMs)) {
			return m.common_na();
		}
		if (durationMs < 1000) {
			return m.activity_duration_ms({ ms: Math.max(0, Math.round(durationMs)) });
		}

		const totalSeconds = Math.round(durationMs / 1000);
		if (totalSeconds < 60) {
			return m.activity_duration_seconds({ seconds: totalSeconds });
		}

		const minutes = Math.floor(totalSeconds / 60);
		const seconds = totalSeconds % 60;
		return m.activity_duration_minutes({ minutes, seconds });
	}

	function formatOutputLineInternal(message: ActivityMessage): string {
		const timestamp = new Intl.DateTimeFormat(undefined, {
			hour: 'numeric',
			minute: '2-digit',
			second: '2-digit'
		}).format(new Date(message.createdAt));
		return `[${timestamp}] ${message.level.toUpperCase()} ${message.message}`;
	}

	function messageLevelClassInternal(level: ActivityMessage['level']): string {
		switch (level) {
			case 'error':
				return 'text-red-300';
			case 'warning':
				return 'text-amber-300';
			case 'success':
				return 'text-emerald-300';
			default:
				return 'text-zinc-100';
		}
	}
</script>

<div class="bg-muted/25 border-border/50 border-b px-4 py-3">
	<div class="border-border/60 bg-background overflow-hidden rounded-lg border shadow-sm">
		<div class="space-y-4 px-5 py-4">
			<div class="flex min-w-0 items-start justify-between gap-4">
				<div class="flex min-w-0 items-start gap-3">
					<div class="bg-primary/10 text-primary flex size-9 shrink-0 items-center justify-center rounded-md">
						<IconComponent class="size-4.5" aria-hidden="true" />
					</div>
					<div class="min-w-0">
						<div class="flex flex-wrap items-center gap-2">
							<h3 class="truncate text-sm font-semibold">{activityTypeLabel(liveActivity.type)}</h3>
							<StatusBadge
								text={activityStatusLabel(liveActivity.status)}
								variant={activityStatusVariant(liveActivity.status)}
								size="sm"
								minWidth="none"
							/>
						</div>
						<p class="text-muted-foreground mt-1 truncate text-xs">{activityTarget}</p>
					</div>
				</div>
				{#if cancelable}
					<IfPermitted perm="activities:cancel">
						<button
							type="button"
							onclick={() => confirmCancelActivity(liveActivity.id)}
							disabled={activityStore.isCancelling(liveActivity.id)}
							class="border-border/60 text-muted-foreground hover:bg-destructive/10 hover:text-destructive hover:border-destructive/30 focus-visible:ring-ring inline-flex shrink-0 items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-xs font-medium transition focus-visible:ring-2 focus-visible:outline-hidden disabled:pointer-events-none disabled:opacity-50"
						>
							<CloseIcon class="size-3.5" aria-hidden="true" />
							{m.activity_cancel()}
						</button>
					</IfPermitted>
				{/if}
			</div>

			<div class="space-y-2">
				<div class="flex items-center justify-between gap-3 text-xs">
					<span class="text-muted-foreground">{liveActivity.step || m.activity_step_unknown()}</span>
					<span class="text-muted-foreground tabular-nums">
						{#if hasProgress}
							{m.activity_progress_percent({ progress: progressValue })}
						{:else}
							{m.common_live()}
						{/if}
					</span>
				</div>
				<Progress
					value={hasProgress ? progressValue : 100}
					indeterminate={!hasProgress && (liveActivity.status === 'running' || liveActivity.status === 'queued')}
					class="h-1.5"
				/>
			</div>

			<div class="text-muted-foreground flex flex-wrap items-center gap-x-4 gap-y-1.5 text-xs">
				<div class="flex items-center gap-1.5">
					<span>{m.common_started()}</span>
					<span class="text-foreground font-medium tabular-nums">{formatDateTimeInternal(liveActivity.startedAt)}</span>
				</div>
				<span class="text-border">•</span>
				<div class="flex items-center gap-1.5">
					<span>{m.common_finished()}</span>
					<span class="text-foreground font-medium tabular-nums">{formatDateTimeInternal(liveActivity.endedAt)}</span>
				</div>
				<span class="text-border">•</span>
				<div class="flex items-center gap-1.5">
					<span>{m.activity_duration()}</span>
					<span class="text-foreground font-medium tabular-nums">{formatDurationInternal(liveActivity)}</span>
				</div>
				{#if sourceEnvironmentName}
					<span class="text-border">•</span>
					<div class="flex items-center gap-1.5">
						<span>{m.activity_source_environment()}</span>
						<span class="text-foreground font-medium">{sourceEnvironmentName}</span>
					</div>
				{/if}
				{#if startedByName}
					<span class="text-border">•</span>
					<div class="flex items-center gap-1.5">
						<span>{m.activity_started_by_label()}</span>
						<span class="text-foreground font-medium">{startedByName}</span>
					</div>
				{/if}
			</div>

			{#if liveActivity.error}
				<div class="border-destructive/30 bg-destructive/10 text-destructive rounded-md border p-3 text-sm">
					{liveActivity.error}
				</div>
			{/if}
		</div>

		<div class="border-border/60 border-t">
			<div class="flex items-center justify-between px-5 py-2.5">
				<div class="flex items-center gap-2">
					<TerminalIcon class="text-muted-foreground size-4" aria-hidden="true" />
					<span class="text-sm font-semibold">{m.activity_output_title()}</span>
				</div>
				<CopyButton text={outputText} variant="ghost" size="default" class="h-8 px-2 text-xs" tabindex={0}>
					<span class="text-xs">{m.activity_copy_output()}</span>
				</CopyButton>
			</div>

			<div
				bind:this={outputContainer}
				class="max-h-80 min-h-40 overflow-auto bg-zinc-950 px-5 py-4 font-mono text-[12px] leading-relaxed text-zinc-100"
			>
				{#if isDetailError && messages.length === 0}
					<div class="flex min-h-32 flex-col items-center justify-center gap-2 text-zinc-500">
						<span>{m.activity_output_load_failed()}</span>
						<button
							type="button"
							onclick={() => activityStore.retryLoadDetail(activity.id)}
							class="text-primary hover:text-primary/80 text-xs underline"
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
					{#each messages as message (message.id)}
						<div class="grid grid-cols-[auto_auto_minmax(0,1fr)] gap-2 rounded px-1 py-0.5 hover:bg-white/[0.04]">
							<span class="text-zinc-500">{formatDateTimeInternal(message.createdAt)}</span>
							<span class={cn('font-bold', messageLevelClassInternal(message.level))}>
								{message.level.toUpperCase()}
							</span>
							<span class="break-words whitespace-pre-wrap">{message.message}</span>
						</div>
					{/each}
				{/if}
			</div>
		</div>
	</div>
</div>
